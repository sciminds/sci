package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
		i, req := i, req
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

	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	tags := in.Tags
	if tags == nil {
		tags = defaultTags
	}

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

		if item.Plan.Action == ActionSkip {
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
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
	var nCached int
	for i := range in.Items {
		if outcomes[i].Action == ActionCreate && outcomes[i].FromCache {
			nCached++
		}
	}

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

		// Build a pdfPath→needExtract index for result matching.
		pdfToIdx := make(map[string]int, len(pdfPaths))
		for pi, idx := range needExtract {
			pdfToIdx[pdfPaths[pi]] = idx
		}

		jobs := in.Jobs
		if jobs < 1 {
			jobs = 1
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
			}
			chunkResults := make([]chunkResult, len(chunks))
			var wg sync.WaitGroup
			for ci, chunk := range chunks {
				ci, chunk := ci, chunk
				wg.Add(1)
				go func() {
					defer wg.Done()
					if ctx.Err() != nil {
						chunkResults[ci] = chunkResult{chunk: chunk, err: ctx.Err()}
						return
					}
					opts := in.ExtractOpts
					jobDir := filepath.Join(outputDir, fmt.Sprintf("batch-%d-job-%d", batchNum, ci))
					opts.OutputDir = jobDir
					res, err := in.Extractor.ExtractBatch(ctx, opts, chunk, in.OnProgress)
					chunkResults[ci] = chunkResult{chunk: chunk, res: res, err: err}
				}()
			}
			wg.Wait()
			batchNum++

			// Process results from this batch's chunks.
			for _, cr := range chunkResults {
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

				// Populate cache for successfully extracted items.
				for _, pdf := range cr.chunk {
					idx := pdfToIdx[pdf]
					item := in.Items[idx]
					extRes, ok := cr.res.Results[pdf]
					if !ok {
						outcomes[idx].Err = fmt.Errorf("docling produced no output for %s", item.Request.PDFName)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
						continue
					}
					md, err := os.ReadFile(extRes.MarkdownPath)
					if err != nil {
						outcomes[idx].Err = fmt.Errorf("read markdown for cache: %w", err)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
						continue
					}
					if _, err := in.Cache.Put(item.Request.PDFKey, item.Hash, md); err != nil {
						outcomes[idx].Err = fmt.Errorf("cache put: %w", err)
						if in.OnItemDone != nil {
							in.OnItemDone(idx, outcomes[idx])
						}
						continue
					}
				}
			}
		}
	}

	// ── Phase 4: post notes for freshly extracted items ──
	// Count how many fresh items need posting (no error, not from cache).
	var nFresh int
	for i := range in.Items {
		if outcomes[i].Err == nil && outcomes[i].Action == ActionCreate && !outcomes[i].FromCache {
			nFresh++
		}
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
	var chunks [][]string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
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
	var chunks [][]string
	for i := 0; i < len(s); i += chunkSize {
		end := i + chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// BatchJobsDefault suggests a worker count for the PlanBatch hashing
// phase based on the target docling device.
func BatchJobsDefault(device string, numCPU int) int {
	switch device {
	case "cpu":
		jobs := numCPU / 4
		if jobs < 1 {
			jobs = 1
		}
		return jobs
	case "", "auto", "mps", "cuda":
		return 1
	default:
		return 1
	}
}
