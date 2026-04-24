package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// writeStubPDF drops a minimal file at path so HashPDF has something
// deterministic to hash.
func writeStubPDF(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPlanBatch_MixedOutcomes: the batch contains one Create, one
// Skip (existing docling note), and one planning failure (bad PDF path).
func TestPlanBatch_MixedOutcomes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Paper A: fresh, no existing note → Create.
	pdfA := filepath.Join(dir, "A", "a.pdf")
	writeStubPDF(t, pdfA, "aaa")
	hashA, err := HashPDF(pdfA)
	if err != nil {
		t.Fatal(err)
	}

	// Paper B: existing docling note → Skip.
	pdfB := filepath.Join(dir, "B", "b.pdf")
	writeStubPDF(t, pdfB, "bbb")

	// Paper C: PDF missing on disk → plan error.
	pdfC := filepath.Join(dir, "C", "missing.pdf")

	hasExisting := map[string]bool{"PB": true}
	reqs := []BatchRequest{
		{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
		{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFPath: pdfB},
		{ParentKey: "PC", PDFKey: "PDFC", PDFName: "c.pdf", PDFPath: pdfC},
	}
	items := PlanBatch(context.Background(), reqs, 2, false, hasExisting)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}

	// Check order preserved.
	for i, want := range []string{"PA", "PB", "PC"} {
		if items[i].Request.ParentKey != want {
			t.Errorf("items[%d].ParentKey = %q, want %q", i, items[i].Request.ParentKey, want)
		}
	}

	// A: Create, hash=hashA.
	if items[0].Err != nil {
		t.Errorf("A: unexpected err %v", items[0].Err)
	} else if items[0].Plan.Action != ActionCreate {
		t.Errorf("A: action = %v, want Create", items[0].Plan.Action)
	} else if items[0].Hash != hashA {
		t.Errorf("A: hash = %q, want %q", items[0].Hash, hashA)
	}

	// B: Skip.
	if items[1].Err != nil {
		t.Errorf("B: unexpected err %v", items[1].Err)
	} else if items[1].Plan.Action != ActionSkip {
		t.Errorf("B: action = %v, want Skip", items[1].Plan.Action)
	}

	// C: plan error, Plan nil.
	if items[2].Err == nil {
		t.Error("C: expected error for missing PDF")
	}
	if items[2].Plan != nil {
		t.Error("C: Plan must be nil on error")
	}
}

// TestExecuteBatch_HappyPath: 2 items, 1 Create + 1 Skip. The create
// goes through ExtractBatch (single docling call), gets cached, and
// the note is posted. Skip never triggers extraction.
func TestExecuteBatch_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	pdfB := filepath.Join(dir, "b.pdf")
	for _, p := range []string{pdfA, pdfB} {
		writeStubPDF(t, p, filepath.Base(p))
	}

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
		{
			Request: BatchRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFPath: pdfB},
			Hash:    "hb",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFHash: "hb"},
				Action:  ActionSkip,
			},
		},
	}

	ex := &fakeExtractor{md: "# Body\n", version: "docling 2.86.0"}
	w := &fakeNoteWriter{}
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    w,
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	created, skipped, cached, failed := res.Counts()
	if created != 1 || skipped != 1 || failed != 0 {
		t.Errorf("counts = created=%d/skipped=%d/cached=%d/failed=%d; want 1/1/0/0", created, skipped, cached, failed)
	}
	// ExtractBatch called once (not per-item).
	if atomic.LoadInt32(&ex.calls) != 1 {
		t.Errorf("extractor calls = %d, want 1 (single batch call)", atomic.LoadInt32(&ex.calls))
	}
	if len(w.created) != 1 || w.created[0].parent != "PA" {
		t.Errorf("CreateChildNote calls = %v", w.created)
	}

	// Cache populated for the non-skip item.
	if _, ok := cache.Get("PDFA", "ha"); !ok {
		t.Error("cache missing PDFA")
	}
}

// TestExecuteBatch_PerItemErrorsContinue: one item has a plan error —
// the batch keeps running for other items.
func TestExecuteBatch_PerItemErrorsContinue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{Request: BatchRequest{ParentKey: "BAD"}, Err: errors.New("bad hash")},
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# h\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err == nil {
		t.Error("BAD: expected carried error in outcome")
	}
	if res.Outcomes[1].Err != nil {
		t.Errorf("PA: unexpected error %v", res.Outcomes[1].Err)
	}
	created, _, _, failed := res.Counts()
	if created != 1 || failed != 1 {
		t.Errorf("created=%d failed=%d; want 1/1", created, failed)
	}
}

// TestExecuteBatch_ExtractorFailureMarksAllPending: if the single
// ExtractBatch call fails entirely, all items needing extraction
// are marked failed but the result is returned (not an error).
func TestExecuteBatch_ExtractorFailureMarksAllPending(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 3
	items := make([]BatchItem, N)
	for i := 0; i < N; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("b%d", i))
		items[i] = BatchItem{
			Request: BatchRequest{
				ParentKey: fmt.Sprintf("P%d", i),
				PDFKey:    fmt.Sprintf("PDF%d", i),
				PDFName:   fmt.Sprintf("p%d.pdf", i),
				PDFPath:   p,
			},
			Hash: fmt.Sprintf("h%d", i),
			Plan: &Plan{
				Request: PlanRequest{
					ParentKey: fmt.Sprintf("P%d", i),
					PDFKey:    fmt.Sprintf("PDF%d", i),
					PDFName:   fmt.Sprintf("p%d.pdf", i),
					PDFHash:   fmt.Sprintf("h%d", i),
				},
				Action: ActionCreate,
			},
		}
	}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{err: errors.New("docling exploded")},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range res.Outcomes {
		if o.Err == nil {
			t.Errorf("outcome[%d] succeeded; expected failure", i)
		}
	}
}

// TestExecuteBatch_FiresCallbacks: OnItemDone fires for every item.
func TestExecuteBatch_FiresCallbacks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdf := filepath.Join(dir, "p.pdf")
	writeStubPDF(t, pdf, "p")
	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFPath: pdf},
			Hash:    "h",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
				Action:  ActionCreate,
			},
		},
	}

	var dones atomic.Int32

	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "x", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
		OnItemDone: func(i int, o BatchOutcome) {
			dones.Add(1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dones.Load() != 1 {
		t.Errorf("dones=%d, want 1", dones.Load())
	}
}

// TestExecuteBatch_CacheHitSkipsExtractor: items already in cache
// skip the docling call entirely but still post notes.
func TestExecuteBatch_CacheHitSkipsExtractor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	if _, err := cache.Put("PDFA", "ha", []byte("## cached\n")); err != nil {
		t.Fatal(err)
	}

	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	ex := &fakeExtractor{md: "unused", version: "docling 2.86.0"}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    w,
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Extractor should NOT be called — everything was cached.
	if atomic.LoadInt32(&ex.calls) != 0 {
		t.Errorf("extractor calls = %d, want 0 (all cached)", atomic.LoadInt32(&ex.calls))
	}
	if res.Outcomes[0].Err != nil {
		t.Errorf("unexpected error: %v", res.Outcomes[0].Err)
	}
	if !res.Outcomes[0].FromCache {
		t.Error("FromCache=false, want true")
	}
	// Note should still be posted.
	if len(w.created) != 1 {
		t.Fatalf("CreateChildNote calls = %d, want 1", len(w.created))
	}
	if !strings.Contains(w.created[0].body, "cached") {
		t.Errorf("posted body missing cached markdown:\n%s", w.created[0].body)
	}
}

// TestExecuteBatch_CachePreservedOnWriterError: if extraction succeeds
// but the note post fails, the cache entry must survive.
func TestExecuteBatch_CachePreservedOnWriterError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# body\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{createErr: errors.New("api 500")},
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err == nil {
		t.Fatal("expected writer error")
	}
	// Cache must be preserved for resume.
	if _, ok := cache.Get("PDFA", "ha"); !ok {
		t.Error("cache entry was dropped after writer failure — resume is broken")
	}
}

// TestExecuteBatch_OnProgressFires: the progress callback fires
// during extraction.
func TestExecuteBatch_OnProgressFires(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	var progressCalls atomic.Int32
	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "body", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
		OnProgress: func(ev *DoclingEvent) {
			progressCalls.Add(1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if progressCalls.Load() == 0 {
		t.Error("OnProgress never fired")
	}
}

// TestExecuteBatch_ParallelJobs: with Jobs=3 and 6 items needing
// extraction, ExtractBatch should be called 3 times (2+2+2) in
// parallel.
func TestExecuteBatch_ParallelJobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 6
	items := make([]BatchItem, N)
	for i := 0; i < N; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("body%d", i))
		items[i] = BatchItem{
			Request: BatchRequest{
				ParentKey: fmt.Sprintf("P%d", i),
				PDFKey:    fmt.Sprintf("PDF%d", i),
				PDFName:   fmt.Sprintf("p%d.pdf", i),
				PDFPath:   p,
			},
			Hash: fmt.Sprintf("h%d", i),
			Plan: &Plan{
				Request: PlanRequest{
					ParentKey: fmt.Sprintf("P%d", i),
					PDFKey:    fmt.Sprintf("PDF%d", i),
					PDFName:   fmt.Sprintf("p%d.pdf", i),
					PDFHash:   fmt.Sprintf("h%d", i),
				},
				Action: ActionCreate,
			},
		}
	}

	ex := &fakeExtractor{md: "# chunk\n", version: "docling 2.86.0"}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    w,
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:      3,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// 6 items / 3 jobs = 3 ExtractBatch calls with 2 PDFs each.
	if atomic.LoadInt32(&ex.calls) != 3 {
		t.Errorf("extractor calls = %d, want 3 (3 parallel jobs)", atomic.LoadInt32(&ex.calls))
	}
	created, _, _, failed := res.Counts()
	if created != 6 || failed != 0 {
		t.Errorf("created=%d failed=%d; want 6/0", created, failed)
	}
	if len(w.created) != 6 {
		t.Errorf("notes posted = %d, want 6", len(w.created))
	}
}

// TestExecuteBatch_SingleJobDefault: Jobs=0 (default) sends all PDFs
// in a single ExtractBatch call.
func TestExecuteBatch_SingleJobDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 4
	items := make([]BatchItem, N)
	for i := 0; i < N; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("body%d", i))
		items[i] = BatchItem{
			Request: BatchRequest{
				ParentKey: fmt.Sprintf("P%d", i),
				PDFKey:    fmt.Sprintf("PDF%d", i),
				PDFName:   fmt.Sprintf("p%d.pdf", i),
				PDFPath:   p,
			},
			Hash: fmt.Sprintf("h%d", i),
			Plan: &Plan{
				Request: PlanRequest{
					ParentKey: fmt.Sprintf("P%d", i),
					PDFKey:    fmt.Sprintf("PDF%d", i),
					PDFName:   fmt.Sprintf("p%d.pdf", i),
					PDFHash:   fmt.Sprintf("h%d", i),
				},
				Action: ActionCreate,
			},
		}
	}

	ex := &fakeExtractor{md: "# all\n", version: "docling 2.86.0"}
	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:      0, // default — single process
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&ex.calls) != 1 {
		t.Errorf("extractor calls = %d, want 1 (all in one batch)", atomic.LoadInt32(&ex.calls))
	}
}

// TestChunkByJobs verifies the chunking arithmetic.
func TestChunkByJobs(t *testing.T) {
	t.Parallel()
	s := []string{"a", "b", "c", "d", "e"}

	// 1 job → single chunk with all items.
	got := chunkByJobs(s, 1)
	if len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("1 job: got %d chunks, want 1", len(got))
	}

	// 2 jobs → chunks of [3, 2].
	got = chunkByJobs(s, 2)
	if len(got) != 2 {
		t.Fatalf("2 jobs: got %d chunks, want 2", len(got))
	}
	if len(got[0]) != 3 || len(got[1]) != 2 {
		t.Errorf("2 jobs: chunk sizes = [%d, %d], want [3, 2]", len(got[0]), len(got[1]))
	}

	// 5 jobs → 5 chunks of 1 each.
	got = chunkByJobs(s, 5)
	if len(got) != 5 {
		t.Fatalf("5 jobs: got %d chunks, want 5", len(got))
	}
	for i, c := range got {
		if len(c) != 1 {
			t.Errorf("5 jobs: chunk[%d] size = %d, want 1", i, len(c))
		}
	}

	// More jobs than items → capped to len(s) chunks.
	got = chunkByJobs(s, 10)
	if len(got) != 5 {
		t.Errorf("10 jobs on 5 items: got %d chunks, want 5", len(got))
	}

	// 0 jobs → single chunk.
	got = chunkByJobs(s, 0)
	if len(got) != 1 {
		t.Errorf("0 jobs: got %d chunks, want 1", len(got))
	}
}

// TestBatchJobsDefault: the heuristic returns 1 for GPU devices and
// NumCPU/4 for CPU.
func TestBatchJobsDefault(t *testing.T) {
	t.Parallel()
	if got := BatchJobsDefault("mps", 8); got != 1 {
		t.Errorf("mps, 8CPU → %d, want 1", got)
	}
	if got := BatchJobsDefault("cuda", 16); got != 1 {
		t.Errorf("cuda, 16CPU → %d, want 1", got)
	}
	if got := BatchJobsDefault("auto", 8); got != 1 {
		t.Errorf("auto, 8CPU → %d, want 1", got)
	}
	if got := BatchJobsDefault("cpu", 8); got != 2 {
		t.Errorf("cpu, 8CPU → %d, want 2", got)
	}
	if got := BatchJobsDefault("cpu", 2); got != 1 {
		t.Errorf("cpu, 2CPU → %d, want 1 (floor)", got)
	}
}

var _ = runtime.NumCPU

// TestExecuteBatch_CachesAfterExtraction verifies that docling output
// files are read and cached after ExtractBatch returns.
func TestExecuteBatch_CachesAfterExtraction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	pdfA := filepath.Join(dir, "a.pdf")
	pdfB := filepath.Join(dir, "b.pdf")
	writeStubPDF(t, pdfA, "aaa")
	writeStubPDF(t, pdfB, "bbb")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
		{
			Request: BatchRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFPath: pdfB},
			Hash:    "hb",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFHash: "hb"},
				Action:  ActionCreate,
			},
		},
	}

	ex := &fakeExtractor{md: "# body\n", version: "docling 2.86.0"}
	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    &fakeNoteWriter{},
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cache.Get("PDFA", "ha"); !ok {
		t.Error("PDFA not cached")
	}
	if _, ok := cache.Get("PDFB", "hb"); !ok {
		t.Error("PDFB not cached")
	}
}

// TestChunkBySize verifies the fixed-size chunking.
func TestChunkBySize(t *testing.T) {
	t.Parallel()
	s := []string{"a", "b", "c", "d", "e"}

	// Chunk size 2 → [2, 2, 1].
	got := chunkBySize(s, 2)
	if len(got) != 3 {
		t.Fatalf("size=2: got %d chunks, want 3", len(got))
	}
	if len(got[0]) != 2 || len(got[1]) != 2 || len(got[2]) != 1 {
		t.Errorf("size=2: chunk sizes = [%d, %d, %d], want [2, 2, 1]",
			len(got[0]), len(got[1]), len(got[2]))
	}

	// Chunk size larger than input → single chunk.
	got = chunkBySize(s, 100)
	if len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("size=100: got %d chunks, want 1", len(got))
	}

	// Chunk size 0 → single chunk (no panic).
	got = chunkBySize(s, 0)
	if len(got) != 1 {
		t.Errorf("size=0: got %d chunks, want 1", len(got))
	}

	// Empty input.
	got = chunkBySize(nil, 5)
	if len(got) != 1 || len(got[0]) != 0 {
		t.Errorf("nil input: got %d chunks", len(got))
	}
}

// TestExecuteBatch_PhaseOrder: with a mix of cached and fresh items,
// OnPhase fires in the expected order (PostCached → Extract →
// PostFresh) and each phase reports the correct count.
func TestExecuteBatch_PhaseOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	// Item A: cached from a prior run.
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")
	if _, err := cache.Put("PDFA", "ha", []byte("## cached A\n")); err != nil {
		t.Fatal(err)
	}

	// Item B: needs extraction.
	pdfB := filepath.Join(dir, "b.pdf")
	writeStubPDF(t, pdfB, "b")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
		{
			Request: BatchRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFPath: pdfB},
			Hash:    "hb",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFHash: "hb"},
				Action:  ActionCreate,
			},
		},
	}

	type phaseEvent struct {
		phase BatchPhase
		count int
	}
	var phases []phaseEvent

	// Track the order of note posts relative to phases.
	var postLog []string

	ex := &fakeExtractor{md: "# fresh\n", version: "docling 2.86.0"}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    w,
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
		OnPhase: func(phase BatchPhase, count int) {
			phases = append(phases, phaseEvent{phase, count})
		},
		OnItemDone: func(i int, o BatchOutcome) {
			if o.NoteKey != "" {
				postLog = append(postLog, o.Item.Request.ParentKey)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify phase order.
	if len(phases) != 3 {
		t.Fatalf("got %d phases, want 3; phases: %+v", len(phases), phases)
	}
	wantPhases := []phaseEvent{
		{PhasePostCached, 1},
		{PhaseExtract, 1},
		{PhasePostFresh, 1},
	}
	for i, want := range wantPhases {
		if phases[i] != want {
			t.Errorf("phase[%d] = %+v, want %+v", i, phases[i], want)
		}
	}

	// Verify PA (cached) was posted before PB (fresh).
	if len(postLog) != 2 {
		t.Fatalf("postLog = %v, want 2 entries", postLog)
	}
	if postLog[0] != "PA" || postLog[1] != "PB" {
		t.Errorf("post order = %v, want [PA, PB] (cached first)", postLog)
	}

	// Both notes should have been posted.
	created, _, _, failed := res.Counts()
	if created != 2 || failed != 0 {
		t.Errorf("created=%d failed=%d; want 2/0", created, failed)
	}
}

// TestExecuteBatch_CachedOnlySkipsExtract: when all items are cached,
// PhaseExtract should not fire and the extractor should not be called.
func TestExecuteBatch_CachedOnlySkipsExtract(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")
	if _, err := cache.Put("PDFA", "ha", []byte("## cached\n")); err != nil {
		t.Fatal(err)
	}

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	type phaseEvent struct {
		phase BatchPhase
		count int
	}
	var phases []phaseEvent
	ex := &fakeExtractor{md: "unused", version: "docling 2.86.0"}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    &fakeNoteWriter{},
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
		OnPhase: func(phase BatchPhase, count int) {
			phases = append(phases, phaseEvent{phase, count})
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Only PostCached should fire — no Extract, no PostFresh.
	if len(phases) != 1 {
		t.Fatalf("got %d phases, want 1; phases: %+v", len(phases), phases)
	}
	if phases[0].phase != PhasePostCached || phases[0].count != 1 {
		t.Errorf("phase = %+v, want {PostCached, 1}", phases[0])
	}
	if atomic.LoadInt32(&ex.calls) != 0 {
		t.Errorf("extractor calls = %d, want 0", atomic.LoadInt32(&ex.calls))
	}
	created, _, _, _ := res.Counts()
	if created != 1 {
		t.Errorf("created=%d, want 1", created)
	}
}

// TestExecuteBatch_FreshOnlySkipsPostCached: when no items are cached,
// PhasePostCached should not fire.
func TestExecuteBatch_FreshOnlySkipsPostCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	type phaseEvent struct {
		phase BatchPhase
		count int
	}
	var phases []phaseEvent

	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# body\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
		OnPhase: func(phase BatchPhase, count int) {
			phases = append(phases, phaseEvent{phase, count})
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Extract + PostFresh, no PostCached.
	if len(phases) != 2 {
		t.Fatalf("got %d phases, want 2; phases: %+v", len(phases), phases)
	}
	if phases[0].phase != PhaseExtract {
		t.Errorf("phase[0] = %+v, want Extract", phases[0])
	}
	if phases[1].phase != PhasePostFresh {
		t.Errorf("phase[1] = %+v, want PostFresh", phases[1])
	}
}

// TestExecuteBatch_TagsParentAfterFreshPost: every successful note post
// for a freshly-extracted item triggers AddTagToItem(parent, MarkdownTag).
// The tag is what powers Zotero saved searches like "PDFs without an
// extraction" without a separate backfill pass for newly-posted items.
func TestExecuteBatch_TagsParentAfterFreshPost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	w := &fakeNoteWriter{}
	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# body\n", version: "docling 2.86.0"},
		Writer:    w,
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(w.tagged) != 1 {
		t.Fatalf("AddTagToItem calls = %d, want 1; tagged=%+v", len(w.tagged), w.tagged)
	}
	if w.tagged[0].item != "PA" || w.tagged[0].tag != MarkdownTag {
		t.Errorf("tagged[0] = %+v, want {PA, %s}", w.tagged[0], MarkdownTag)
	}
}

// TestExecuteBatch_TagsParentAfterCachedPost: cached items posted in
// PhasePostCached also get the parent tag — the tagging hook lives in
// postNote so both phases benefit identically.
func TestExecuteBatch_TagsParentAfterCachedPost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	if _, err := cache.Put("PDFA", "ha", []byte("## cached\n")); err != nil {
		t.Fatal(err)
	}
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	w := &fakeNoteWriter{}
	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "unused", version: "docling 2.86.0"},
		Writer:    w,
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(w.tagged) != 1 || w.tagged[0].item != "PA" {
		t.Errorf("tagged = %+v, want one call for PA", w.tagged)
	}
}

// TestExecuteBatch_TagFailureDoesNotFailPost: if AddTagToItem errors
// after CreateChildNote succeeded, the post is still recorded as
// successful (NoteKey set, no outcome.Err). The retroactive backfill
// sweep on the next --apply will heal the missing tag.
func TestExecuteBatch_TagFailureDoesNotFailPost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")

	items := []BatchItem{
		{
			Request: BatchRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
			Hash:    "ha",
			Plan: &Plan{
				Request: PlanRequest{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFHash: "ha"},
				Action:  ActionCreate,
			},
		},
	}

	w := &fakeNoteWriter{tagErr: errors.New("412 conflict")}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# body\n", version: "docling 2.86.0"},
		Writer:    w,
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err != nil {
		t.Errorf("outcome.Err = %v, want nil (tag failure must not fail the post)", res.Outcomes[0].Err)
	}
	if res.Outcomes[0].NoteKey == "" {
		t.Error("NoteKey empty: post was recorded as failed despite CreateChildNote success")
	}
	if len(w.created) != 1 {
		t.Errorf("CreateChildNote calls = %d, want 1", len(w.created))
	}
}
