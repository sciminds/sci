package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeChildLister serves a fixed response per parent key so PlanBatch
// can be tested without an HTTP client.
type fakeChildLister struct {
	children map[string][]ChildNote
	err      error
}

func (f *fakeChildLister) ListNoteChildren(_ context.Context, parentKey string) ([]ChildNote, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.children[parentKey], nil
}

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
// Skip (matching sentinel), and one planning failure (bad PDF path).
// PlanBatch must return all three in order, each with the right shape.
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

	// Paper B: existing note already matches → Skip.
	pdfB := filepath.Join(dir, "B", "b.pdf")
	writeStubPDF(t, pdfB, "bbb")
	hashB, err := HashPDF(pdfB)
	if err != nil {
		t.Fatal(err)
	}
	existingBody := "<p>hi</p><!-- sci-extract:PDFB:" + hashB + " -->"

	// Paper C: PDF missing on disk → plan error.
	pdfC := filepath.Join(dir, "C", "missing.pdf")

	lister := &fakeChildLister{children: map[string][]ChildNote{
		"PB": {{Key: "NOTE_B_OLD", Body: existingBody}},
	}}
	reqs := []BatchRequest{
		{ParentKey: "PA", PDFKey: "PDFA", PDFName: "a.pdf", PDFPath: pdfA},
		{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFPath: pdfB},
		{ParentKey: "PC", PDFKey: "PDFC", PDFName: "c.pdf", PDFPath: pdfC},
	}
	items := PlanBatch(context.Background(), lister, reqs, 2, false)
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

// TestExecuteBatch_HappyPath: 3 items, 1 Create + 1 Replace + 1 Skip,
// with 2 workers. Skip never calls extractor; the others do and post.
func TestExecuteBatch_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	pdfB := filepath.Join(dir, "b.pdf")
	pdfC := filepath.Join(dir, "c.pdf")
	for _, p := range []string{pdfA, pdfB, pdfC} {
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
				Request:      PlanRequest{ParentKey: "PB", PDFKey: "PDFB", PDFName: "b.pdf", PDFHash: "hb"},
				Action:       ActionReplace,
				ExistingNote: "OLDB",
			},
		},
		{
			Request: BatchRequest{ParentKey: "PC", PDFKey: "PDFC", PDFName: "c.pdf", PDFPath: pdfC},
			Hash:    "hc",
			Plan: &Plan{
				Request:      PlanRequest{ParentKey: "PC", PDFKey: "PDFC", PDFName: "c.pdf", PDFHash: "hc"},
				Action:       ActionSkip,
				ExistingNote: "CURRENTC",
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
		Jobs:      2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Aborted {
		t.Errorf("batch should not be aborted: %s", res.AbortReason)
	}

	created, replaced, skipped, cached, failed := res.Counts()
	if created != 1 || replaced != 1 || skipped != 1 || failed != 0 {
		t.Errorf("counts = %d/%d/%d/cached=%d/failed=%d; want 1/1/1/0/0", created, replaced, skipped, cached, failed)
	}
	if ex.calls != 2 {
		t.Errorf("extractor calls = %d, want 2 (Skip must not trigger)", ex.calls)
	}
	if len(w.created) != 1 || w.created[0].parent != "PA" {
		t.Errorf("CreateChildNote calls = %v", w.created)
	}
	if len(w.updated) != 1 || w.updated[0].key != "OLDB" {
		t.Errorf("UpdateChildNote calls = %v", w.updated)
	}

	// Cache populated for the non-skip items.
	if _, ok := cache.Get("PDFA", "ha"); !ok {
		t.Error("cache missing PDFA")
	}
	if _, ok := cache.Get("PDFB", "hb"); !ok {
		t.Error("cache missing PDFB")
	}
}

// TestExecuteBatch_PerItemErrorsContinue: one item fails (plan
// carried an error) — the batch keeps running and reports the failure
// in its outcome without aborting.
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
		Jobs:      1,
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
	created, _, _, _, failed := res.Counts()
	if created != 1 || failed != 1 {
		t.Errorf("created=%d failed=%d; want 1/1", created, failed)
	}
}

// TestExecuteBatch_CircuitBreakerAborts: every item fails, the
// consecutive-failure limit trips, and the remaining unprocessed
// items get a cancellation error.
func TestExecuteBatch_CircuitBreakerAborts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 10
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
		Items:                   items,
		Extractor:               &fakeExtractor{err: errors.New("docling exploded")},
		Writer:                  &fakeNoteWriter{},
		Cache:                   &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:                    1, // serial so "consecutive" is well-defined
		ConsecutiveFailureLimit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Aborted {
		t.Error("expected Aborted=true after circuit breaker trip")
	}
	// Every outcome should carry an error — the first few from the
	// extractor explosion, the rest from the cancellation.
	for i, o := range res.Outcomes {
		if o.Err == nil {
			t.Errorf("outcome[%d] succeeded; expected failure or cancel", i)
		}
	}
}

// TestExecuteBatch_FiresCallbacks: OnItemStart / OnItemDone fire once
// per item and the completion count matches Items.
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

	var starts, dones atomic.Int32
	var mu sync.Mutex
	var doneOutcome BatchOutcome

	_, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "x", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:      1,
		OnItemStart: func(i int, it BatchItem) {
			starts.Add(1)
		},
		OnItemDone: func(i int, o BatchOutcome) {
			dones.Add(1)
			mu.Lock()
			doneOutcome = o
			mu.Unlock()
		},
		Now: func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 || dones.Load() != 1 {
		t.Errorf("starts=%d dones=%d, want 1/1", starts.Load(), dones.Load())
	}
	if doneOutcome.NoteKey == "" {
		t.Error("OnItemDone fired with empty NoteKey")
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

// Compile-time guard: runtime.NumCPU is used by the CLI layer for the
// default jobs computation; keep the import tangible so a future
// refactor doesn't silently drop it.
var _ = runtime.NumCPU
