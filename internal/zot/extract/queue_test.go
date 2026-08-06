package extract

// Concurrency tests for the extraction work queue: a channel-gated fake
// extractor lets the test control exactly when each document finishes,
// so the no-head-of-line-blocking guarantee is asserted with real
// synchronization, never sleeps.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedExtractor blocks each document on a per-stem gate channel the
// test closes to release it. started/finished record progress.
type gatedExtractor struct {
	md       string
	version  string
	started  chan string              // stem pushed on entry; buffer ≥ total docs
	finished chan string              // stem pushed after outputs written
	gates    map[string]chan struct{} // stem → closed by the test to release
	failFor  string                   // stem whose chunk errors after its gate opens

	inFlight atomic.Int32
	peak     atomic.Int32

	mu      sync.Mutex
	batches [][]string
}

func newGatedExtractor(total int, stems []string) *gatedExtractor {
	g := &gatedExtractor{
		md:       "# body\n",
		version:  "docling 9.9.9",
		started:  make(chan string, total),
		finished: make(chan string, total),
		gates:    map[string]chan struct{}{},
	}
	for _, s := range stems {
		g.gates[s] = make(chan struct{})
	}
	return g
}

func (g *gatedExtractor) release(stem string) { close(g.gates[stem]) }

func (g *gatedExtractor) Extract(context.Context, ExtractOptions) (*ExtractResult, error) {
	return nil, errors.New("gatedExtractor: not scripted")
}

func (g *gatedExtractor) ExtractBatch(ctx context.Context, opts ExtractOptions, pdfs []string, onProgress ProgressFunc) (*BatchExtractResult, error) {
	cur := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		p := g.peak.Load()
		if cur <= p || g.peak.CompareAndSwap(p, cur) {
			break
		}
	}
	g.mu.Lock()
	g.batches = append(g.batches, slices.Clone(pdfs))
	g.mu.Unlock()

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}
	results := map[string]*ExtractResult{}
	for _, pdf := range pdfs {
		stem := stemFor(pdf)
		g.started <- stem
		select {
		case <-g.gates[stem]:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if stem == g.failFor {
			return nil, errors.New("gated: docling died")
		}
		mdPath := filepath.Join(opts.OutputDir, stem+".md")
		if err := os.WriteFile(mdPath, []byte(g.md), 0o644); err != nil {
			return nil, err
		}
		if onProgress != nil {
			onProgress(&DoclingEvent{Kind: EventFinished, Document: stem + ".pdf", Duration: time.Second})
			onProgress(&DoclingEvent{Kind: EventOutput, OutputPath: mdPath})
		}
		results[pdf] = &ExtractResult{MarkdownPath: mdPath, ToolVersion: g.version, Duration: time.Second}
		g.finished <- stem
	}
	return &BatchExtractResult{Results: results, ToolVersion: g.version, Duration: time.Second}, nil
}

// mustRecv receives from ch with a deadlock guard. The time.After is a
// test timeout, not synchronization — real progress is channel-driven.
func mustRecv(t *testing.T, ch chan string, label string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("deadlock waiting for %s", label)
		return ""
	}
}

// queueFixture builds items with injected page costs keyed by stem.
func queueFixture(t *testing.T, costs map[string]int) ([]BatchItem, func(string) int, string) {
	t.Helper()
	dir := t.TempDir()
	var items []BatchItem
	for stem := range costs {
		p := filepath.Join(dir, stem+".pdf")
		writeStubPDF(t, p, stem)
		items = append(items, mkBatchItem("P"+stem, "PDF"+stem, stem+".pdf", p, "h"+stem, ActionCreate))
	}
	est := func(path string) int { return costs[stemFor(path)] }
	return items, est, dir
}

// TestExecuteBatch_NoHeadOfLineBlocking is the workstream's headline: a
// 400-page book on one worker must not stop 8 papers from flowing
// through the other. All papers complete while the book's gate is still
// closed.
func TestExecuteBatch_NoHeadOfLineBlocking(t *testing.T) {
	t.Parallel()
	costs := map[string]int{"book": 400}
	var papers []string
	for i := 1; i <= 8; i++ {
		s := fmt.Sprintf("p%d", i)
		papers = append(papers, s)
		costs[s] = 10
	}
	items, est, dir := queueFixture(t, costs)

	stems := slices.Concat([]string{"book"}, papers)
	ex := newGatedExtractor(len(stems), stems)
	w := &fakeNoteWriter{}

	done := make(chan *BatchResult, 1)
	go func() {
		res, err := ExecuteBatch(context.Background(), BatchInput{
			Items:         items,
			Extractor:     ex,
			Writer:        w,
			Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
			Jobs:          2,
			PageEstimator: est,
			Now:           func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Errorf("ExecuteBatch: %v", err)
		}
		done <- res
	}()

	// Two workers: one pulls the isolated book chunk, the other the
	// first paper chunk. The set is deterministic even if the order
	// isn't.
	first := map[string]bool{mustRecv(t, ex.started, "first start"): true}
	first[mustRecv(t, ex.started, "second start")] = true
	if !first["book"] {
		t.Fatalf("first pulls = %v — the isolated book chunk must start immediately", first)
	}

	// Release every paper in turn WITHOUT releasing the book. Each
	// release lets the paper worker proceed to the next document (or
	// the next chunk once the pool chunk drains).
	released := 0
	for released < len(papers) {
		var cur string
		for s := range first {
			if s != "book" {
				cur = s
			}
		}
		if cur == "" {
			t.Fatal("no paper in flight while book blocks — head-of-line blocking")
		}
		ex.release(cur)
		if got := mustRecv(t, ex.finished, cur+" finish"); got != cur {
			t.Fatalf("finished %s, want %s", got, cur)
		}
		released++
		delete(first, cur)
		if released < len(papers) {
			first[mustRecv(t, ex.started, "next paper start")] = true
		}
	}

	// Every paper finished while the book's gate never opened.
	ex.release("book")
	if got := mustRecv(t, ex.finished, "book finish"); got != "book" {
		t.Fatalf("finished %s, want book", got)
	}

	res := <-done
	if res == nil {
		t.Fatal("nil result")
	}
	created, _, _, failed, _ := res.Counts()
	if created != 9 || failed != 0 {
		t.Errorf("created=%d failed=%d, want 9/0", created, failed)
	}
	// The book never shared an invocation with a paper.
	ex.mu.Lock()
	defer ex.mu.Unlock()
	for _, b := range ex.batches {
		hasBook := false
		for _, pdf := range b {
			if stemFor(pdf) == "book" {
				hasBook = true
			}
		}
		if hasBook && len(b) != 1 {
			t.Errorf("book shared a chunk: %v", b)
		}
	}
}

// TestExecuteBatch_WorkerCountBoundsConcurrency: Jobs=2 must never have
// more than 2 docling invocations in flight, however many chunks exist.
func TestExecuteBatch_WorkerCountBoundsConcurrency(t *testing.T) {
	t.Parallel()
	costs := map[string]int{}
	var stems []string
	for i := range 6 {
		s := fmt.Sprintf("b%d", i)
		stems = append(stems, s)
		costs[s] = 100 // all isolated → 6 chunks
	}
	items, est, dir := queueFixture(t, costs)
	ex := newGatedExtractor(len(stems), stems)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := ExecuteBatch(context.Background(), BatchInput{
			Items:         items,
			Extractor:     ex,
			Writer:        &fakeNoteWriter{},
			Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
			Jobs:          2,
			PageEstimator: est,
			Now:           func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Errorf("ExecuteBatch: %v", err)
		}
	}()

	// Exactly two documents start; releasing each lets the worker pull
	// the next chunk.
	inflight := []string{mustRecv(t, ex.started, "start 1"), mustRecv(t, ex.started, "start 2")}
	for released := 0; released < len(stems); {
		cur := inflight[0]
		inflight = inflight[1:]
		ex.release(cur)
		mustRecv(t, ex.finished, cur+" finish")
		released++
		if released <= len(stems)-2 {
			inflight = append(inflight, mustRecv(t, ex.started, "next start"))
		}
	}
	<-done
	if p := ex.peak.Load(); p != 2 {
		t.Errorf("peak concurrent invocations = %d, want 2", p)
	}
}

// TestExecuteBatch_FewerChunksThanJobs: surplus workers exit instead of
// deadlocking on the pre-closed queue.
func TestExecuteBatch_FewerChunksThanJobs(t *testing.T) {
	t.Parallel()
	items, est, dir := queueFixture(t, map[string]int{"a": 5, "b": 5})
	ex := &fakeExtractor{md: "# body\n", version: "docling 9.9.9"}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:         items,
		Extractor:     ex,
		Writer:        &fakeNoteWriter{},
		Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:          16,
		PageEstimator: est,
		Now:           func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&ex.calls) != 1 {
		t.Errorf("extractor calls = %d, want 1 (both docs pool into one chunk)", atomic.LoadInt32(&ex.calls))
	}
	created, _, _, failed, _ := res.Counts()
	if created != 2 || failed != 0 {
		t.Errorf("created=%d failed=%d, want 2/0", created, failed)
	}
}

// TestExecuteBatch_CancelMidQueue: canceling stops workers from pulling
// further chunks; ExecuteBatch returns without hanging.
func TestExecuteBatch_CancelMidQueue(t *testing.T) {
	t.Parallel()
	costs := map[string]int{}
	var stems []string
	for i := range 6 {
		s := fmt.Sprintf("c%d", i)
		stems = append(stems, s)
		costs[s] = 100 // isolated → 6 chunks
	}
	items, est, dir := queueFixture(t, costs)
	ex := newGatedExtractor(len(stems), stems)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := ExecuteBatch(ctx, BatchInput{
			Items:         items,
			Extractor:     ex,
			Writer:        &fakeNoteWriter{},
			Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
			Jobs:          2,
			PageEstimator: est,
			Now:           func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Errorf("ExecuteBatch: %v", err)
		}
	}()

	// Two chunks in flight; cancel while both are gated. The gated
	// extractor returns ctx.Err(), workers see the cancel and stop.
	mustRecv(t, ex.started, "start 1")
	mustRecv(t, ex.started, "start 2")
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ExecuteBatch hung after cancel")
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if len(ex.batches) >= 6 {
		t.Errorf("all %d chunks were handed to the extractor despite cancel", len(ex.batches))
	}
}

// TestExecuteBatch_OneChunkFailureDoesNotPoisonOthers: the book's
// docling process dies; the papers' chunks still complete and post.
func TestExecuteBatch_OneChunkFailureDoesNotPoisonOthers(t *testing.T) {
	t.Parallel()
	costs := map[string]int{"book": 400, "p1": 10, "p2": 10}
	items, est, dir := queueFixture(t, costs)
	stems := []string{"book", "p1", "p2"}
	ex := newGatedExtractor(len(stems), stems)
	ex.failFor = "book"
	for _, s := range stems {
		ex.release(s) // everything free-runs; the book chunk errors
	}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:         items,
		Extractor:     ex,
		Writer:        w,
		Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:          2,
		PageEstimator: est,
		Now:           func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if byKey["Pbook"].Err == nil {
		t.Error("book chunk error not recorded")
	}
	for _, k := range []string{"Pp1", "Pp2"} {
		if byKey[k].Err != nil {
			t.Errorf("%s poisoned by the book chunk: %v", k, byKey[k].Err)
		}
	}
	if len(w.created) != 2 {
		t.Errorf("posted = %d, want 2 papers", len(w.created))
	}
}
