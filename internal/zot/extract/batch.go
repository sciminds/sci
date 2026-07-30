package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
)

// BatchRequest describes a single parent item to extract in a bulk
// run. Populated by the CLI from the local library query —
// everything is pre-resolved on disk so the batch layer only deals
// with hashing, planning, extracting, and posting.
type BatchRequest struct {
	ParentKey string
	PDFKey    string
	PDFName   string
	PDFPath   string // absolute on-disk PDF
}

// BatchItem is one request after the plan phase: its computed PDF
// hash, the PlanExtract decision, and a per-item error if planning
// failed (hash IO, …). Batch never aborts on a plan error — it
// records the error and moves on, mirroring Execute's error-per-item
// behavior.
type BatchItem struct {
	Request BatchRequest
	Hash    string
	Plan    *Plan
	// Err is set when hashing failed. When non-nil, Plan is nil and
	// ExecuteBatch treats the item as a failure without invoking
	// docling or the writer.
	Err error
}

// PlanBatch resolves a PDF hash and calls PlanExtract for every
// request, with up to `jobs` operations in flight. Results are
// returned in the same order as the input so the caller can correlate
// indices with progress callbacks.
//
// hasExisting is the set of parent keys that already have a
// docling-tagged child note in the local DB.
func PlanBatch(ctx context.Context, reqs []BatchRequest, jobs int, force bool, hasExisting map[string]bool) []BatchItem {
	if jobs < 1 {
		jobs = 1
	}
	out := make([]BatchItem, len(reqs))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				out[i] = BatchItem{Request: req, Err: ctx.Err()}
				return
			}
			hash, err := HashPDF(req.PDFPath)
			if err != nil {
				out[i] = BatchItem{Request: req, Err: fmt.Errorf("hash %s: %w", req.PDFPath, err)}
				return
			}
			plan := PlanExtract(PlanRequest{
				ParentKey: req.ParentKey,
				PDFKey:    req.PDFKey,
				PDFName:   req.PDFName,
				PDFHash:   hash,
				Force:     force,
			}, hasExisting[req.ParentKey])
			out[i] = BatchItem{Request: req, Hash: hash, Plan: plan}
		}()
	}
	wg.Wait()
	return out
}

// BatchInput carries everything ExecuteBatch needs. Populated by the
// CLI layer after PlanBatch returns.
type BatchInput struct {
	// Items from PlanBatch. Items with non-nil Err are counted as
	// failed; items with Plan.Action == ActionSkip are counted as
	// skipped and never touch the extractor.
	Items []BatchItem
	// Extractor runs docling (or a fake in tests). Called once via
	// ExtractBatch with all PDF paths that need extraction.
	Extractor Extractor
	// Writer posts the notes. In cache-only mode (no --apply), the
	// CLI passes a noop writer.
	Writer NoteWriter
	// Cache is the markdown cache used for crash-resume. Required:
	// the whole point of ExecuteBatch is to never re-run docling on
	// work we've already done, so callers must pass a valid cache.
	Cache *MarkdownCache
	// ExtractOpts is the docling option set. ExecuteBatch sets
	// OutputDir before passing it to ExtractBatch.
	ExtractOpts ExtractOptions
	// RenderHTML, when true, renders the docling markdown as HTML via
	// goldmark before posting. The default (false) stores raw markdown.
	RenderHTML bool
	// Tags applied to newly created notes. Nil → default ["docling"].
	Tags []string
	// Jobs is the number of worker goroutines pulling chunks off the
	// extraction queue; each chunk is one docling invocation. Every
	// docling process loads models independently (~20-40s) and holds
	// ~14 GB resident, so keep jobs × NumThreads at or under the core
	// count. 0 means 1. See planChunks for how work is ordered and how
	// oversize documents get isolated so they can't block papers.
	Jobs int
	// PageEstimator returns the per-document scheduling cost in pages
	// for one PDF path. Nil → EstimatePages. Injected by tests to make
	// the queue deterministic without real PDFs.
	PageEstimator func(pdfPath string) int
	// OutputDir is where docling writes all its output for the batch.
	// ExecuteBatch creates this if needed.
	OutputDir string
	// Layout, when non-nil, activates persistent per-key artifact mode:
	// every extraction runs in md+json+referenced-image form over a
	// staged KEY.pdf symlink and is finalized into Layout's per-parent-key
	// dirs. Resume is driven by Layout.Done (not the markdown cache,
	// which can't reproduce the DoclingDocument JSON), and items whose
	// Plan says Skip (existing Zotero note) are STILL extracted when
	// their layout dir is missing — the note and the layout are
	// independent stores. Nil means classic behavior.
	//
	// Finalization is per-document, during extraction (see
	// layoutFinalizer): a chunk that errors or is canceled banks every
	// document docling finished, so an interrupted run resumes from the
	// in-flight tail, never from zero.
	Layout *KeyLayout
	// Now is injected for tests. Nil → time.Now.
	Now func() time.Time
	// OnProgress fires for each docling log event during extraction.
	// Safe to be nil.
	OnProgress ProgressFunc
	// OnItemDone fires when an item's note is posted (or skipped/failed).
	// Safe to be nil.
	OnItemDone func(i int, outcome BatchOutcome)
	// OnPhase fires when ExecuteBatch transitions between phases. The
	// CLI uses this to update the progress bar title and total. Safe
	// to be nil.
	OnPhase func(phase BatchPhase, count int)
}

// BatchPhase identifies which stage ExecuteBatch is in.
type BatchPhase int

const (
	// PhasePostCached — posting notes for previously-extracted cached items.
	PhasePostCached BatchPhase = iota
	// PhaseEstimate — measuring PDF page counts to order the queue.
	PhaseEstimate
	// PhaseExtract — running docling on un-cached PDFs.
	PhaseExtract
	// PhasePostFresh — posting notes for newly-extracted items.
	PhasePostFresh
)

// BatchOutcome is what ExecuteBatch produced for one item.
type BatchOutcome struct {
	Index     int
	Item      BatchItem
	NoteKey   string
	Action    Action
	FromCache bool
	Duration  time.Duration
	Err       error
	// LayoutWritten is true when this run finalized the item's
	// per-key layout dir (layout mode only; false for dirs that
	// were already Done).
	LayoutWritten bool
}

// BatchResult is the full return value of ExecuteBatch. Outcomes is
// aligned 1:1 with Input.Items.
type BatchResult struct {
	Outcomes    []BatchOutcome
	ToolVersion string
}

// Counts returns the tallies used by CLI result rendering.
func (r *BatchResult) Counts() (created, skipped, cached, failed int) {
	for _, o := range r.Outcomes {
		if o.Err != nil {
			failed++
			continue
		}
		switch o.Action {
		case ActionCreate:
			created++
		case ActionSkip:
			skipped++
		}
		if o.FromCache {
			cached++
		}
	}
	return
}

// ExecuteBatch extracts every un-cached PDF and posts notes.
//
// Flow:
//  1. Partition items into: skip, cached (cache hit), extract (need docling).
//  2. Post notes for cached items first (flushes prior runs to Zotero
//     before starting new extraction work).
//  3. Estimate page counts, order the work longest-first, and drain it
//     with Jobs workers — one docling invocation per chunk, oversize
//     documents isolated (planChunks), each document banked as docling
//     completes it (cache in classic mode, per-key layout dirs via
//     layoutFinalizer in layout mode).
//  4. Post notes for freshly extracted items.
func ExecuteBatch(ctx context.Context, in BatchInput) (*BatchResult, error) {
	if in.Extractor == nil {
		return nil, errors.New("batch: Extractor required")
	}
	if in.Writer == nil {
		return nil, errors.New("batch: Writer required")
	}
	if in.Cache == nil {
		return nil, errors.New("batch: Cache required (resume needs it)")
	}

	outcomes := make([]BatchOutcome, len(in.Items))
	result := &BatchResult{Outcomes: outcomes}

	now := lo.Ternary(in.Now != nil, in.Now, time.Now)
	tags := lo.Ternary(in.Tags != nil, in.Tags, defaultTags)

	// ── Phase 1: classify each item ──
	// Indices of items that need docling extraction.
	var needExtract []int
	// PDF paths for the single ExtractBatch call.
	var pdfPaths []string

	for i, item := range in.Items {
		outcomes[i] = BatchOutcome{Index: i, Item: item}

		if item.Err != nil {
			outcomes[i].Err = item.Err
			outcomes[i].Action = ActionCreate // attempted but failed
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
		}
		if item.Plan == nil {
			outcomes[i].Err = errors.New("batch: nil Plan with no Err")
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
		}

		outcomes[i].Action = item.Plan.Action

		layoutDone := in.Layout != nil && in.Layout.Done(item.Request.ParentKey)

		// A Skip plan (existing Zotero note) only ends the item's journey
		// when there is no layout to fill: in layout mode a missing dir
		// still needs the extraction, just not the note.
		if item.Plan.Action == ActionSkip && (in.Layout == nil || layoutDone) {
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
		}

		if in.Layout != nil {
			if layoutDone {
				// Note needed, extraction already on disk — serve the
				// note body from the layout markdown via the cache the
				// posting phase reads.
				if _, ok := in.Cache.Get(item.Request.PDFKey, item.Hash); !ok {
					md, err := os.ReadFile(in.Layout.MarkdownPath(item.Request.ParentKey))
					if err == nil {
						_, err = in.Cache.Put(item.Request.PDFKey, item.Hash, md)
					}
					if err != nil {
						outcomes[i].Err = fmt.Errorf("read layout markdown for %s: %w", item.Request.ParentKey, err)
						if in.OnItemDone != nil {
							in.OnItemDone(i, outcomes[i])
						}
						continue
					}
				}
				outcomes[i].FromCache = true
				continue
			}
			// The markdown cache is deliberately NOT consulted here: a
			// cached placeholder-mode markdown can't produce the
			// DoclingDocument JSON the layout requires.
			needExtract = append(needExtract, i)
			pdfPaths = append(pdfPaths, item.Request.PDFPath)
			continue
		}

		// Check cache.
		if _, ok := in.Cache.Get(item.Request.PDFKey, item.Hash); ok {
			outcomes[i].FromCache = true
			continue
		}

		// Needs extraction.
		needExtract = append(needExtract, i)
		pdfPaths = append(pdfPaths, item.Request.PDFPath)
	}

	// Count cached items for phase notification.
	nCached := lo.CountBy(outcomes, func(o BatchOutcome) bool {
		return o.Action == ActionCreate && o.FromCache
	})

	// ── Phase 2: post notes for cached items first ──
	// Flushes prior extraction runs to Zotero before starting new
	// docling work, so partial runs make incremental progress.
	if nCached > 0 {
		if in.OnPhase != nil {
			in.OnPhase(PhasePostCached, nCached)
		}
		for i, item := range in.Items {
			if outcomes[i].Err != nil || outcomes[i].Action != ActionCreate || !outcomes[i].FromCache {
				continue
			}
			postNote(ctx, i, item, in, outcomes, result, tags, now)
		}
	}

	// ── Phase 3: run docling over un-cached PDFs ──
	// Work is estimated (pages), ordered longest-first, and drained by
	// Jobs workers from one queue — one docling invocation per chunk,
	// with oversize documents isolated so a scanned monograph can never
	// sit at the head of a queue of papers (see planChunks).
	if len(pdfPaths) > 0 {
		outputDir := in.OutputDir
		if outputDir == "" {
			tmp, err := os.MkdirTemp("", "sci-extract-batch-*")
			if err != nil {
				return nil, fmt.Errorf("batch: mkdir temp: %w", err)
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			outputDir = tmp
		}

		// Layout mode: extract in full form (markdown + DoclingDocument
		// + referenced images) over KEY.pdf symlinks so every docling
		// output comes out key-named. Tables are NOT requested here —
		// Finalize derives tables/ from the JSON itself.
		extractOpts := in.ExtractOpts
		if in.Layout != nil {
			extractOpts.Formats = []OutputFormat{FormatMarkdown, FormatJSON}
			extractOpts.ImageMode = ImageReferenced
			extractOpts.TablesAsCSV = false

			stagingDir := filepath.Join(outputDir, "staging")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				return nil, fmt.Errorf("batch: mkdir staging: %w", err)
			}
			keptIdx := needExtract[:0]
			keptPaths := pdfPaths[:0]
			for pi, idx := range needExtract {
				staged, err := StageKeyPDF(stagingDir, in.Items[idx].Request.ParentKey, pdfPaths[pi])
				if err != nil {
					outcomes[idx].Err = err
					if in.OnItemDone != nil {
						in.OnItemDone(idx, outcomes[idx])
					}
					continue
				}
				keptIdx = append(keptIdx, idx)
				keptPaths = append(keptPaths, staged)
			}
			needExtract, pdfPaths = keptIdx, keptPaths
		}

		// Build a pdfPath→needExtract index for result matching.
		pdfToIdx := make(map[string]int, len(pdfPaths))
		for pi, idx := range needExtract {
			pdfToIdx[pdfPaths[pi]] = idx
		}

		// Build stem→(pdfPath, item index) so the progress callback
		// can cache each document as soon as docling writes it.
		stemToInfo := make(map[string]stemInfo, len(pdfPaths))
		for pi, idx := range needExtract {
			stemToInfo[stemFor(pdfPaths[pi])] = stemInfo{pdfPath: pdfPaths[pi], idx: idx}
		}

		// Estimate each document's cost (pages) to order the queue.
		// Estimates read the REAL PDF via the item, not the staged
		// symlink — the truth is the same file, but the estimator stays
		// independent of staging order.
		est := in.PageEstimator
		if est == nil {
			est = EstimatePages
		}
		if in.OnPhase != nil {
			in.OnPhase(PhaseEstimate, len(pdfPaths))
		}
		costs := make([]int, len(pdfPaths))
		estSem := make(chan struct{}, min(runtime.NumCPU(), 8))
		var estWG sync.WaitGroup
		for pi := range pdfPaths {
			if ctx.Err() != nil {
				break
			}
			estWG.Add(1)
			estSem <- struct{}{}
			go func() {
				defer estWG.Done()
				defer func() { <-estSem }()
				costs[pi] = est(in.Items[needExtract[pi]].Request.PDFPath)
			}()
		}
		estWG.Wait()

		if in.OnPhase != nil {
			// Fired after staging and estimation so the total is the
			// true number of documents docling will see.
			in.OnPhase(PhaseExtract, len(pdfPaths))
		}

		// LPT queue: chunks come out biggest-work-first, with oversize
		// documents isolated (see planChunks). A pre-filled, pre-closed
		// channel means workers never block and never deadlock when
		// there are fewer chunks than workers.
		chunks := planChunks(costs, isolateChunkPages, chunkTargetDocs)
		queue := make(chan int, len(chunks))
		for ci := range chunks {
			queue <- ci
		}
		close(queue)

		cc := &chunkContext{
			in:          in,
			extractOpts: extractOpts,
			outputDir:   outputDir,
			pdfPaths:    pdfPaths,
			stemToInfo:  stemToInfo,
			pdfToIdx:    pdfToIdx,
			outcomes:    outcomes,
			versions:    make([]string, len(chunks)),
		}
		var wg sync.WaitGroup
		for range effectiveJobs(max(in.Jobs, 1), len(chunks)) {
			wg.Go(func() {
				for ci := range queue {
					if ctx.Err() != nil {
						// Unpulled chunks are simply not run; the CLI
						// collapses an interrupted run into one error.
						return
					}
					runChunk(ctx, cc, ci, chunks[ci])
				}
			})
		}
		wg.Wait()

		// First non-empty per-chunk version wins; the slots are written
		// by exactly one worker each, and only read here after Wait.
		if v, ok := lo.Find(cc.versions, func(v string) bool { return v != "" }); ok {
			result.ToolVersion = v
		}
	}

	// ── Phase 4: post notes for freshly extracted items ──
	// Count how many fresh items need posting (no error, not from cache).
	nFresh := lo.CountBy(outcomes, func(o BatchOutcome) bool {
		return o.Err == nil && o.Action == ActionCreate && !o.FromCache
	})
	// Salvage can post notes for documents whose chunk process died
	// before reporting a version (an interrupt kills docling mid-batch).
	// Record the tool without one rather than emitting an empty source.
	// No-op in classic mode: a wholesale-failed chunk leaves no fresh
	// error-free outcomes there.
	if result.ToolVersion == "" && nFresh > 0 {
		result.ToolVersion = "docling"
	}
	if nFresh > 0 {
		if in.OnPhase != nil {
			in.OnPhase(PhasePostFresh, nFresh)
		}
		for i, item := range in.Items {
			if outcomes[i].Err != nil || outcomes[i].Action != ActionCreate || outcomes[i].FromCache {
				continue
			}
			postNote(ctx, i, item, in, outcomes, result, tags, now)
		}
	}

	return result, nil
}

// stemInfo locates one queued document by its output stem: the input
// pdf path and its index into BatchInput.Items.
type stemInfo struct {
	pdfPath string
	idx     int
}

// chunkContext is the read-only state every runChunk call shares. The
// mutable slices (outcomes, versions) are written on disjoint indices
// only — planChunks partitions the slot space and each chunk index is
// pulled by exactly one worker — so no lock is needed.
type chunkContext struct {
	in          BatchInput
	extractOpts ExtractOptions
	outputDir   string
	pdfPaths    []string
	stemToInfo  map[string]stemInfo
	pdfToIdx    map[string]int
	outcomes    []BatchOutcome
	versions    []string
}

// runChunk executes one docling invocation over the chunk's slots and
// post-processes its results: layout mode banks documents incrementally
// via layoutFinalizer and salvages completed ones when the chunk errors
// or is canceled; classic mode caches markdown and fails the chunk
// wholesale on error (the cache is its resume store). Writes only its
// own slots' outcomes and versions[ci].
func runChunk(ctx context.Context, cc *chunkContext, ci int, slots []int) {
	in := cc.in
	chunk := lo.Map(slots, func(pi int, _ int) string { return cc.pdfPaths[pi] })
	jobDir := filepath.Join(cc.outputDir, fmt.Sprintf("chunk-%03d", ci))
	var fin *layoutFinalizer
	if in.Layout != nil {
		fin = newLayoutFinalizer(in, jobDir, chunk, cc.pdfToIdx, cc.outcomes)
	}

	opts := cc.extractOpts
	opts.OutputDir = jobDir

	// Bank each document as docling completes it, for crash resilience.
	// Classic mode caches the markdown; layout mode goes further and
	// finalizes the whole per-key dir (see layoutFinalizer). Docling
	// logs an output path slightly before the file is flushed, so both
	// paths queue on one event and drain on the next — by then the
	// previous file is on disk.
	var pending []string
	onEvent := func(ev *DoclingEvent) {
		if fin != nil {
			fin.onEvent(ev)
		} else {
			// Drain: try to cache any queued paths.
			still := pending[:0]
			for _, p := range pending {
				stem := strings.TrimSuffix(filepath.Base(p), ".md")
				if info, ok := cc.stemToInfo[stem]; ok {
					if md, err := os.ReadFile(p); err == nil {
						item := in.Items[info.idx]
						_, _ = in.Cache.Put(item.Request.PDFKey, item.Hash, md)
					} else {
						still = append(still, p)
					}
				}
			}
			pending = still

			// Enqueue new output path.
			if ev.Kind == EventOutput && strings.HasSuffix(ev.OutputPath, ".md") {
				pending = append(pending, ev.OutputPath)
			}
		}

		if in.OnProgress != nil {
			in.OnProgress(ev)
		}
	}

	res, err := in.Extractor.ExtractBatch(ctx, opts, chunk, onEvent)

	if in.Layout != nil {
		// Sweep before judging: everything docling actually finished is
		// on disk, so an errored or canceled chunk still banks its
		// completed documents. Only the remainder fails.
		for _, stem := range fin.sweep() {
			idx := fin.stems[stem]
			item := in.Items[idx]
			if err != nil {
				cc.outcomes[idx].Err = fmt.Errorf("batch extract: %w", err)
			} else {
				cc.outcomes[idx].Err = fmt.Errorf("docling produced no output for %s", item.Request.PDFName)
			}
			if in.OnItemDone != nil {
				in.OnItemDone(idx, cc.outcomes[idx])
			}
		}
		if err == nil {
			cc.versions[ci] = res.ToolVersion
		}
		return
	}

	if err != nil {
		for _, pdf := range chunk {
			idx := cc.pdfToIdx[pdf]
			cc.outcomes[idx].Err = fmt.Errorf("batch extract: %w", err)
			if in.OnItemDone != nil {
				in.OnItemDone(idx, cc.outcomes[idx])
			}
		}
		return
	}
	cc.versions[ci] = res.ToolVersion

	// Cache anything the progress callback missed (e.g. single-doc
	// chunks where the file write races with the log line) and mark
	// per-document failures.
	for _, pdf := range chunk {
		idx := cc.pdfToIdx[pdf]
		item := in.Items[idx]

		// Already cached by the progress callback?
		if _, ok := in.Cache.Get(item.Request.PDFKey, item.Hash); ok {
			continue
		}
		docRes, ok := res.Results[pdf]
		if !ok {
			cc.outcomes[idx].Err = fmt.Errorf("docling produced no output for %s", item.Request.PDFName)
			if in.OnItemDone != nil {
				in.OnItemDone(idx, cc.outcomes[idx])
			}
			continue
		}
		// Fallback: read the file now that docling has exited.
		md, err := os.ReadFile(docRes.MarkdownPath)
		if err != nil {
			cc.outcomes[idx].Err = fmt.Errorf("read docling output for %s: %w", item.Request.PDFName, err)
			if in.OnItemDone != nil {
				in.OnItemDone(idx, cc.outcomes[idx])
			}
			continue
		}
		if _, putErr := in.Cache.Put(item.Request.PDFKey, item.Hash, md); putErr != nil {
			cc.outcomes[idx].Err = fmt.Errorf("cache %s: %w", item.Request.PDFName, putErr)
			if in.OnItemDone != nil {
				in.OnItemDone(idx, cc.outcomes[idx])
			}
		}
	}
}

// layoutFinalizer finalizes a chunk's documents into the per-key layout
// as docling completes them, rather than waiting for the chunk's
// process to exit. A chunk is hours of work: before this existed, an
// interrupt failed every PDF in it and the deferred cleanup of the
// staging dir deleted every document that had already converted.
//
// The trigger is EventFinished, but docling logs that line *before* it
// writes the document's output files, so a finished stem is queued and
// completed on a later drain — the same drain-on-next-event pattern the
// markdown cache uses. A stem is only finalized once both KEY.md and
// KEY.json stat successfully. The one-event deferral also means a
// document's outputs are only moved out of the job dir after docling
// has demonstrably moved on to the next document — do not "optimize"
// the drain into an immediate finalize on the JSON output event.
//
// Threading: onEvent runs on the chunk's stderr-scanner goroutine
// (inside Extractor.ExtractBatch); sweep runs on the same worker
// goroutine after that call returned. The two never overlap, so no
// lock is needed. Parallel chunks own disjoint stems, disjoint item
// indices, and disjoint job dirs, so they never contend on outcomes or
// on a key dir.
type layoutFinalizer struct {
	in       BatchInput
	jobDir   string
	outcomes []BatchOutcome
	stems    map[string]int     // stem → index into in.Items, this chunk only
	secs     map[string]float64 // stem → EventFinished duration
	pending  []string           // finished stems whose outputs aren't on disk yet
	done     map[string]bool    // stems already finalized (success or failure)
}

func newLayoutFinalizer(in BatchInput, jobDir string, chunk []string, pdfToIdx map[string]int, outcomes []BatchOutcome) *layoutFinalizer {
	return &layoutFinalizer{
		in: in, jobDir: jobDir, outcomes: outcomes,
		stems: lo.SliceToMap(chunk, func(p string) (string, int) { return stemFor(p), pdfToIdx[p] }),
		secs:  map[string]float64{},
		done:  map[string]bool{},
	}
}

// onEvent drains anything queued by a previous event, then queues this
// event's document if it just finished.
func (f *layoutFinalizer) onEvent(ev *DoclingEvent) {
	f.drain()
	if ev.Kind != EventFinished {
		return
	}
	stem := stemFor(ev.Document)
	if _, ok := f.stems[stem]; !ok {
		return // not one of this chunk's documents
	}
	f.secs[stem] = ev.Duration.Seconds()
	if !f.done[stem] && !slices.Contains(f.pending, stem) {
		f.pending = append(f.pending, stem)
	}
}

// drain finalizes every queued stem whose outputs are now on disk.
func (f *layoutFinalizer) drain() {
	ready, waiting := lo.FilterReject(f.pending, func(stem string, _ int) bool {
		return layoutOutputsReady(f.jobDir, stem)
	})
	f.pending = waiting
	lo.ForEach(ready, func(stem string, _ int) { f.finalize(stem) })
}

// sweep finalizes every document of the chunk that has complete outputs
// on disk but wasn't finalized during the run: the last document (whose
// drain never got a following event), anything lost to a flush race,
// and — when the chunk errored or was canceled — everything docling had
// already converted. It deliberately ignores ctx: the files are on disk
// and banking them is the whole point. Returns the stems it could not
// finalize, sorted, so the caller fails exactly those.
func (f *layoutFinalizer) sweep() []string {
	f.drain()
	stems := lo.Keys(f.stems)
	slices.Sort(stems) // deterministic outcome/callback ordering
	ready, unfinalized := lo.FilterReject(stems, func(stem string, _ int) bool {
		return f.done[stem] || layoutOutputsReady(f.jobDir, stem)
	})
	lo.ForEach(ready, func(stem string, _ int) { f.finalize(stem) }) // no-op for already-done
	return unfinalized
}

// finalize caches the markdown (the posting phase reads it from there,
// and Finalize is about to move the staging file), then writes the
// per-key dir. Skip-action items end their journey here — the posting
// phase won't touch them — so their OnItemDone fires now; Create items
// report when their note posts. The manifest records the document's own
// EventFinished duration; zero means "unknown" (killed mid-document),
// never the chunk total.
func (f *layoutFinalizer) finalize(stem string) {
	if f.done[stem] {
		return
	}
	f.done[stem] = true // set first: a failed finalize must not be retried by sweep
	idx := f.stems[stem]
	item := f.in.Items[idx]
	fail := func(err error) {
		f.outcomes[idx].Err = err
		if f.in.OnItemDone != nil {
			f.in.OnItemDone(idx, f.outcomes[idx])
		}
	}
	if _, ok := f.in.Cache.Get(item.Request.PDFKey, item.Hash); !ok {
		md, err := os.ReadFile(filepath.Join(f.jobDir, stem+".md"))
		if err == nil {
			_, err = f.in.Cache.Put(item.Request.PDFKey, item.Hash, md)
		}
		if err != nil {
			fail(fmt.Errorf("cache %s: %w", item.Request.PDFName, err))
			return
		}
	}
	secs := f.secs[stem]
	if _, err := f.in.Layout.Finalize(item.Request.ParentKey, f.jobDir, item.Request.PDFPath, secs); err != nil {
		fail(err)
		return
	}
	f.outcomes[idx].LayoutWritten = true
	f.outcomes[idx].Duration = time.Duration(secs * float64(time.Second))
	if item.Plan.Action == ActionSkip && f.in.OnItemDone != nil {
		f.in.OnItemDone(idx, f.outcomes[idx])
	}
}

// postNote reads the cached markdown for a single item, renders the
// note body, and posts it to Zotero via the writer. It updates
// outcomes[i] in place and fires OnItemDone.
func postNote(
	ctx context.Context,
	i int,
	item BatchItem,
	in BatchInput,
	outcomes []BatchOutcome,
	result *BatchResult,
	tags []string,
	now func() time.Time,
) {
	cachedPath, ok := in.Cache.Get(item.Request.PDFKey, item.Hash)
	if !ok {
		outcomes[i].Err = fmt.Errorf("expected cache entry for %s after extraction", item.Request.PDFName)
		if in.OnItemDone != nil {
			in.OnItemDone(i, outcomes[i])
		}
		return
	}

	md, err := os.ReadFile(cachedPath)
	if err != nil {
		outcomes[i].Err = fmt.Errorf("read cached markdown: %w", err)
		if in.OnItemDone != nil {
			in.OnItemDone(i, outcomes[i])
		}
		return
	}

	toolVersion := result.ToolVersion
	if outcomes[i].FromCache {
		toolVersion = "docling (cached)"
	}

	meta := NoteMeta{
		ParentKey: item.Plan.Request.ParentKey,
		PDFKey:    item.Plan.Request.PDFKey,
		PDFName:   item.Plan.Request.PDFName,
		DOI:       item.Plan.Request.DOI,
		Source:    toolVersion,
		Hash:      item.Plan.Request.PDFHash,
		Generated: now(),
	}
	var body string
	if in.RenderHTML {
		body = MarkdownToNoteHTML(md, meta)
	} else {
		body = MarkdownToNoteRaw(md, meta)
	}

	key, err := in.Writer.CreateChildNote(ctx, item.Plan.Request.ParentKey, body, tags)
	if err != nil {
		outcomes[i].Err = fmt.Errorf("create note: %w", err)
		if in.OnItemDone != nil {
			in.OnItemDone(i, outcomes[i])
		}
		return
	}
	outcomes[i].NoteKey = key

	// Tag the parent with MarkdownTag so saved searches can target
	// items missing an extraction. Best-effort: a tag failure here
	// (e.g. transient 412) leaves the parent missing the tag, but
	// the next --apply's backfill sweep will re-add it. Failing the
	// post outcome over a tag glitch would be misleading — the note
	// itself was successfully created.
	_ = in.Writer.AddTagToItem(ctx, item.Plan.Request.ParentKey, MarkdownTag)

	if in.OnItemDone != nil {
		in.OnItemDone(i, outcomes[i])
	}
}

// maxDoclingBatch caps how many input PDFs a single docling invocation
// may scan — docling reads every input before starting extraction, so
// thousands of files make it appear hung. It is enforced inside
// planChunks as an upper bound on chunk size (the working size is
// chunkTargetDocs); it is no longer a batching/barrier mechanism.
const maxDoclingBatch = 50

// BatchJobsDefault is the default docling worker count for a device
// (BatchInput.Jobs when neither --jobs nor extract.jobs is set).
//
//   - mps: 2. Apple-silicon unified memory comfortably holds two ~14 GB
//     docling processes, and OCR — the dominant cost on scanned
//     documents — is CPU-bound, so a second process recovers most of
//     the idle capacity a single one leaves.
//   - cuda: 1 — discrete VRAM is the binding constraint.
//   - cpu: numCPU/4, floor 1.
//   - auto/unknown: 1 — an unknown device gets no bump without evidence.
func BatchJobsDefault(device string, numCPU int) int {
	switch device {
	case "cpu":
		return max(numCPU/4, 1)
	case "mps":
		return 2
	default: // "", "auto", "cuda", anything else
		return 1
	}
}
