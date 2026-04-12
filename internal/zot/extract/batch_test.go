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
	if ex.calls != 1 {
		t.Errorf("extractor calls = %d, want 1 (single batch call)", ex.calls)
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
	if ex.calls != 0 {
		t.Errorf("extractor calls = %d, want 0 (all cached)", ex.calls)
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

// TestExecuteBatch_ChunkedExtraction: with BatchSize=2 and 5 items
// needing extraction, ExtractBatch should be called 3 times (2+2+1).
func TestExecuteBatch_ChunkedExtraction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 5
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
		BatchSize: 2,
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// 5 items / batch size 2 = 3 ExtractBatch calls (2+2+1).
	if ex.calls != 3 {
		t.Errorf("extractor calls = %d, want 3 (chunked 2+2+1)", ex.calls)
	}
	created, _, _, failed := res.Counts()
	if created != 5 || failed != 0 {
		t.Errorf("created=%d failed=%d; want 5/0", created, failed)
	}
	if len(w.created) != 5 {
		t.Errorf("notes posted = %d, want 5", len(w.created))
	}
}

// TestExecuteBatch_BatchSizeZeroMeansAll: BatchSize=0 (default) sends
// all PDFs in a single ExtractBatch call.
func TestExecuteBatch_BatchSizeZeroMeansAll(t *testing.T) {
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
		BatchSize: 0, // default — all in one call
		Now:       func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ex.calls != 1 {
		t.Errorf("extractor calls = %d, want 1 (all in one batch)", ex.calls)
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
