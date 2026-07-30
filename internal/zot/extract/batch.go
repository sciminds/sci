package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// Jobs controls how many parallel docling processes to run.
	// 0 or 1 means a single process handles all PDFs (models load
	// once). Higher values split PDFs evenly across N concurrent
	// processes — each loads models independently but they run in
	// parallel. On MPS, 1 is usually best; on CPU, 2-4 can help.
	Jobs int
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

// ExecuteBatch extracts all PDFs in a single docling invocation, then
// populates the cache, then posts notes. This replaces the old
// worker-pool approach: one process means models load once.
//
// Flow:
//  1. Partition items into: skip, cached (cache hit), extract (need docling).
//  2. Post notes for cached items first (flushes prior runs to Zotero
//     before starting new extraction work).
//  3. Run docling in size-limited batches over un-cached PDFs, caching
//     results as each batch completes.
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
	// Docling hangs when given thousands of input files in a single
	// invocation (it scans all inputs before starting extraction).
	// We split into batches of maxDoclingBatch PDFs; each batch is a
	// separate docling process that loads models and starts extracting
	// immediately. Within each batch, Jobs controls parallelism.
	if len(pdfPaths) > 0 {
		if in.OnPhase != nil {
			in.OnPhase(PhaseExtract, len(pdfPaths))
		}
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

		jobs := max(in.Jobs, 1)

		// Build stem→(pdfPath, item index) so the progress callback
		// can cache each document as soon as docling writes it.
		stemToInfo := make(map[string]struct {
			pdfPath string
			idx     int
		}, len(pdfPaths))
		for pi, idx := range needExtract {
			stem := stemFor(pdfPaths[pi])
			stemToInfo[stem] = struct {
				pdfPath string
				idx     int
			}{pdfPaths[pi], idx}
		}

		// Split into size-limited batches, then apply jobs parallelism
		// within each batch.
		batches := chunkBySize(pdfPaths, maxDoclingBatch)
		batchNum := 0
		for _, batch := range batches {
			if ctx.Err() != nil {
				break
			}
			chunks := chunkByJobs(batch, jobs)

			type chunkResult struct {
				chunk []string
				res   *BatchExtractResult
				err   error
				fin   *layoutFinalizer
			}
			chunkResults := make([]chunkResult, len(chunks))
			var wg sync.WaitGroup
			for ci, chunk := range chunks {
				// jobDir and the finalizer are built OUTSIDE the
				// goroutine so cr.fin is never nil — even when the
				// goroutine short-circuits on a canceled ctx, the
				// post-exit sweep still runs over the (empty) job dir.
				jobDir := filepath.Join(outputDir, fmt.Sprintf("batch-%d-job-%d", batchNum, ci))
				var fin *layoutFinalizer
				if in.Layout != nil {
					fin = newLayoutFinalizer(in, jobDir, chunk, pdfToIdx, outcomes)
				}
				wg.Go(func() {
					if ctx.Err() != nil {
						chunkResults[ci] = chunkResult{chunk: chunk, fin: fin, err: ctx.Err()}
						return
					}
					opts := extractOpts
					opts.OutputDir = jobDir

					// Bank each document as docling completes it, for
					// crash resilience. Classic mode caches the
					// markdown; layout mode goes further and finalizes
					// the whole per-key dir (see layoutFinalizer).
					// Docling logs an output path slightly before the
					// file is flushed, so both paths queue on one
					// event and drain on the next — by then the
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
								if info, ok := stemToInfo[stem]; ok {
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
					chunkResults[ci] = chunkResult{chunk: chunk, res: res, err: err, fin: fin}
				})
			}
			wg.Wait()
			batchNum++

			// Process results — cache anything the progress callback
			// missed (e.g. single-doc batches where the file write
			// races with the log line) and mark errors.
			for _, cr := range chunkResults {
				if in.Layout != nil {
					// Sweep before judging: everything docling actually
					// finished is on disk, so an errored or canceled
					// chunk still banks its completed documents. Only
					// the remainder fails.
					for _, stem := range cr.fin.sweep() {
						idx := cr.fin.stems[stem]
						item := in.Items[idx]
						if cr.err != nil {
							outcomes[idx].Err = fmt.Errorf("batch extract: %w", cr.err)
						} else {
							outcomes[idx].Err = fmt.Errorf("docling produced no output for %s", item.Request.PDFName)
						}
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
					}
					// ToolVersion is written only here, on the
					// ExecuteBatch goroutine after wg.Wait() — never
					// from a chunk callback, which would race.
					if cr.err == nil && result.ToolVersion == "" {
						result.ToolVersion = cr.res.ToolVersion
					}
					continue
				}

				if cr.err != nil {
					for _, pdf := range cr.chunk {
						idx := pdfToIdx[pdf]
						outcomes[idx].Err = fmt.Errorf("batch extract: %w", cr.err)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
					}
					continue
				}
				if result.ToolVersion == "" {
					result.ToolVersion = cr.res.ToolVersion
				}

				for _, pdf := range cr.chunk {
					idx := pdfToIdx[pdf]
					item := in.Items[idx]

					// Already cached by the progress callback?
					if _, ok := in.Cache.Get(item.Request.PDFKey, item.Hash); ok {
						continue
					}
					res, ok := cr.res.Results[pdf]
					if !ok {
						outcomes[idx].Err = fmt.Errorf("docling produced no output for %s", item.Request.PDFName)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
						continue
					}
					// Fallback: read the file now that docling has exited.
					md, err := os.ReadFile(res.MarkdownPath)
					if err != nil {
						outcomes[idx].Err = fmt.Errorf("read docling output for %s: %w", item.Request.PDFName, err)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
						continue
					}
					if _, putErr := in.Cache.Put(item.Request.PDFKey, item.Hash, md); putErr != nil {
						outcomes[idx].Err = fmt.Errorf("cache %s: %w", item.Request.PDFName, putErr)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
					}
				}
			}
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
// (inside Extractor.ExtractBatch); sweep runs on the ExecuteBatch
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

// maxDoclingBatch is the maximum number of PDFs passed to a single
// docling invocation. Docling scans all inputs before starting
// extraction, so passing thousands of files causes it to appear hung.
// 50 keeps startup fast (~seconds) while amortising model load time
// across a reasonable number of documents.
const maxDoclingBatch = 50

// chunkBySize splits s into chunks of at most n elements.
func chunkBySize(s []string, n int) [][]string {
	if n <= 0 || len(s) == 0 {
		return [][]string{s}
	}
	return lo.Chunk(s, n)
}

// chunkByJobs splits s into exactly `jobs` roughly-equal chunks.
// jobs ≤ 1 returns s as a single chunk. If jobs > len(s), returns
// one chunk per element.
func chunkByJobs(s []string, jobs int) [][]string {
	if jobs <= 1 || len(s) == 0 {
		return [][]string{s}
	}
	if jobs > len(s) {
		jobs = len(s)
	}
	chunkSize := (len(s) + jobs - 1) / jobs // ceil division
	return lo.Chunk(s, chunkSize)
}

// BatchJobsDefault suggests a worker count for the PlanBatch hashing
// phase based on the target docling device.
func BatchJobsDefault(device string, numCPU int) int {
	switch device {
	case "cpu":
		jobs := max(numCPU/4, 1)
		return jobs
	case "", "auto", "mps", "cuda":
		return 1
	default:
		return 1
	}
}
