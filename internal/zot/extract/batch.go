package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	// BatchSize controls how many PDFs are passed to a single docling
	// process. 0 (default) means all PDFs in one invocation — models
	// load once. Positive values chunk the PDF list into groups of N,
	// one docling process per chunk. Useful when GPU memory is tight
	// (3-7 is the sweet spot on MPS before per-paper throughput
	// degrades vs CPU).
	BatchSize int
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
}

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
//  2. Call ExtractBatch once with all extract-needing PDF paths.
//  3. Populate cache for each newly extracted item.
//  4. Post notes for all create-action items (cached + freshly extracted).
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
			// Will post note in phase 3.
			continue
		}

		// Needs extraction.
		needExtract = append(needExtract, i)
		pdfPaths = append(pdfPaths, item.Request.PDFPath)
	}

	// ── Phase 2: run docling over un-cached PDFs ──
	// Chunk the PDF list: BatchSize=0 means all in one call.
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

		chunks := chunkStrings(pdfPaths, in.BatchSize)
		// Parallel index map: needExtract[i] corresponds to pdfPaths[i].
		// Build a pdfPath→needExtract index for result matching.
		pdfToIdx := make(map[string]int, len(pdfPaths))
		for pi, idx := range needExtract {
			pdfToIdx[pdfPaths[pi]] = idx
		}

		for _, chunk := range chunks {
			if ctx.Err() != nil {
				break
			}

			opts := in.ExtractOpts
			opts.OutputDir = outputDir

			batchRes, err := in.Extractor.ExtractBatch(ctx, opts, chunk, in.OnProgress)
			if err != nil {
				// This chunk failed — mark its items as failed.
				for _, pdf := range chunk {
					idx := pdfToIdx[pdf]
					outcomes[idx].Err = fmt.Errorf("batch extract: %w", err)
					if in.OnItemDone != nil {
						in.OnItemDone(idx, outcomes[idx])
					}
				}
				continue
			}
			if result.ToolVersion == "" {
				result.ToolVersion = batchRes.ToolVersion
			}

			// Populate cache for successfully extracted items in this chunk.
			for _, pdf := range chunk {
				idx := pdfToIdx[pdf]
				item := in.Items[idx]
				extRes, ok := batchRes.Results[pdf]
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

	// ── Phase 3: post notes for all Create items (cached + fresh) ──
	for i, item := range in.Items {
		if outcomes[i].Err != nil || outcomes[i].Action != ActionCreate {
			continue
		}

		cachedPath, ok := in.Cache.Get(item.Request.PDFKey, item.Hash)
		if !ok {
			outcomes[i].Err = fmt.Errorf("expected cache entry for %s after extraction", item.Request.PDFName)
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
		}

		md, err := os.ReadFile(cachedPath)
		if err != nil {
			outcomes[i].Err = fmt.Errorf("read cached markdown: %w", err)
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
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
			continue
		}
		outcomes[i].NoteKey = key
		if in.OnItemDone != nil {
			in.OnItemDone(i, outcomes[i])
		}
	}

	return result, nil
}

// chunkStrings splits s into groups of at most n. n ≤ 0 returns s as
// a single chunk (all items in one batch).
func chunkStrings(s []string, n int) [][]string {
	if n <= 0 || n >= len(s) {
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
