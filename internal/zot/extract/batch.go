package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	// Extractor runs docling (or a fake in tests). Shared across
	// workers — implementations must be goroutine-safe.
	// DoclingExtractor is: each Extract call spawns its own subprocess.
	Extractor Extractor
	// Writer posts the notes.
	Writer NoteWriter
	// Cache is the markdown cache used for crash-resume. Required:
	// the whole point of ExecuteBatch is to never re-run docling on
	// work we've already done, so callers must pass a valid cache.
	Cache *MarkdownCache
	// ExtractOpts is the docling option set. Workers fill in the
	// per-item PDFPath and OutputDir before handing it to Execute.
	ExtractOpts ExtractOptions
	// RenderHTML, when true, renders the docling markdown as HTML via
	// goldmark before posting. The default (false) stores raw markdown.
	RenderHTML bool
	// Tags applied to newly created notes. Nil → default ["docling"].
	Tags []string
	// Jobs is the worker count. <1 means 1 (serial).
	Jobs int
	// TempDirRoot is where each worker creates its docling output
	// scratch dir. Empty → os.TempDir().
	TempDirRoot string
	// ConsecutiveFailureLimit aborts the batch after N completions
	// in a row have all failed. 0 disables the circuit breaker.
	ConsecutiveFailureLimit int
	// Now is injected for tests. Nil → time.Now.
	Now func() time.Time
	// OnItemStart fires when a worker picks up an item. i is the
	// index into Items (stable). Safe to be nil.
	OnItemStart func(i int, item BatchItem)
	// OnItemDone fires when an item completes (success, skip, or
	// failure). Safe to be nil.
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
	Aborted     bool
	AbortReason string
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

// ExecuteBatch runs a worker pool over in.Items, invoking Execute per
// item with its own scratch temp dir. Failures are collected into
// Outcomes rather than aborting the batch, except when the
// consecutive-failure circuit breaker trips.
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
	jobs := in.Jobs
	if jobs < 1 {
		jobs = 1
	}
	tempRoot := in.TempDirRoot
	if tempRoot == "" {
		tempRoot = os.TempDir()
	}

	outcomes := make([]BatchOutcome, len(in.Items))
	result := &BatchResult{Outcomes: outcomes}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var streak atomic.Int32

	type job struct {
		idx  int
		item BatchItem
	}
	jobCh := make(chan job)

	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobCh {
				outcome := runOne(runCtx, in, j.idx, j.item, tempRoot, workerID)
				outcomes[j.idx] = outcome
				if in.OnItemDone != nil {
					in.OnItemDone(j.idx, outcome)
				}
				if outcome.Err != nil {
					newStreak := streak.Add(1)
					if in.ConsecutiveFailureLimit > 0 && int(newStreak) >= in.ConsecutiveFailureLimit {
						result.Aborted = true
						result.AbortReason = fmt.Sprintf("aborted after %d consecutive failures", newStreak)
						cancel()
					}
				} else {
					streak.Store(0)
				}
			}
		}(w)
	}

	for i, item := range in.Items {
		if runCtx.Err() != nil {
			outcomes[i] = BatchOutcome{
				Index: i,
				Item:  item,
				Err:   fmt.Errorf("batch: %s", result.AbortReason),
			}
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
			continue
		}
		if in.OnItemStart != nil {
			in.OnItemStart(i, item)
		}
		select {
		case jobCh <- job{idx: i, item: item}:
		case <-runCtx.Done():
			outcomes[i] = BatchOutcome{
				Index: i,
				Item:  item,
				Err:   fmt.Errorf("batch: %s", result.AbortReason),
			}
			if in.OnItemDone != nil {
				in.OnItemDone(i, outcomes[i])
			}
		}
	}
	close(jobCh)
	wg.Wait()
	return result, nil
}

// runOne handles a single batch item end-to-end.
func runOne(ctx context.Context, in BatchInput, idx int, item BatchItem, tempRoot string, workerID int) BatchOutcome {
	started := time.Now()
	out := BatchOutcome{Index: idx, Item: item}

	if item.Err != nil {
		out.Err = item.Err
		return out
	}
	if item.Plan == nil {
		out.Err = errors.New("batch: nil Plan with no Err")
		return out
	}
	out.Action = item.Plan.Action

	if item.Plan.Action == ActionSkip {
		out.Duration = time.Since(started)
		return out
	}

	tmp, err := os.MkdirTemp(tempRoot, fmt.Sprintf("sci-extract-w%d-*", workerID))
	if err != nil {
		out.Err = fmt.Errorf("mkdir scratch: %w", err)
		return out
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	res, err := Execute(ctx, ExecuteInput{
		Plan:        item.Plan,
		Extractor:   in.Extractor,
		Writer:      in.Writer,
		PDFPath:     item.Request.PDFPath,
		OutputDir:   tmp,
		ExtractOpts: in.ExtractOpts,
		RenderHTML:  in.RenderHTML,
		Tags:        in.Tags,
		Now:         in.Now,
		Cache:       in.Cache,
	})
	out.Duration = time.Since(started)
	if err != nil {
		out.Err = err
		return out
	}
	out.NoteKey = res.NoteKey
	if res.Extraction != nil {
		out.FromCache = res.Extraction.FromCache
	}
	return out
}

// BatchJobsDefault suggests a worker count based on the target
// docling device.
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

// WorkerScratchDir composes a per-worker scratch path under root.
func WorkerScratchDir(root string, workerID int) string {
	return filepath.Join(root, fmt.Sprintf("sci-extract-w%d", workerID))
}
