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
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/lo"
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

// mkBatchItem builds a fully-populated BatchItem whose PlanRequest mirrors
// the BatchRequest fields. The Plan is non-nil with the given Action; the
// outcome Err is left zero (set it separately for error-path tests).
func mkBatchItem(parentKey, pdfKey, pdfName, pdfPath, hash string, action Action) BatchItem {
	return BatchItem{
		Request: BatchRequest{
			ParentKey: parentKey,
			PDFKey:    pdfKey,
			PDFName:   pdfName,
			PDFPath:   pdfPath,
		},
		Hash: hash,
		Plan: &Plan{
			Request: PlanRequest{
				ParentKey: parentKey,
				PDFKey:    pdfKey,
				PDFName:   pdfName,
				PDFHash:   hash,
			},
			Action: action,
		},
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
		mkBatchItem("PB", "PDFB", "b.pdf", pdfB, "hb", ActionSkip),
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

	created, skipped, cached, failed, _ := res.Counts()
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

// TestExecuteBatch_LayoutMode is the end-to-end contract of persistent
// layout mode:
//   - PA (Create, no layout): extracted via a staged KEY.pdf symlink,
//     layout dir written, note posted.
//   - PB (Skip — existing Zotero note — but no layout): still extracted
//     so the corpus gets its dir, but NO note posted.
//   - PC (Create, layout already Done): no extraction; the note body is
//     served from the layout markdown.
func TestExecuteBatch_LayoutMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := &KeyLayout{Dir: filepath.Join(dir, "extracts")}
	if err := os.MkdirAll(layout.Dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-build PC's completed layout dir with distinctive markdown.
	staging := writeStagedOutputs(t, t.TempDir(), "PC")
	if _, err := layout.Finalize("PC", staging, "/old/pc.pdf", 1); err != nil {
		t.Fatal(err)
	}

	var items []BatchItem
	for _, k := range []string{"PA", "PB", "PC"} {
		pdf := filepath.Join(dir, strings.ToLower(k)+".pdf")
		writeStubPDF(t, pdf, k)
		action := ActionCreate
		if k == "PB" {
			action = ActionSkip
		}
		items = append(items, mkBatchItem(k, "PDF"+k, k+".pdf", pdf, "h"+k, action))
	}

	ex := &fakeExtractor{md: "# fresh\n", version: "docling 2.86.0", full: true}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    w,
		Cache:     &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Layout:    layout,
		Now:       func() time.Time { return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Extraction ran over staged key-named symlinks for PA + PB only.
	if n := atomic.LoadInt32(&ex.calls); n != 1 {
		t.Errorf("extractor calls = %d, want 1", n)
	}
	var stems []string
	for _, batch := range ex.batches {
		for _, p := range batch {
			stems = append(stems, stemFor(p))
		}
	}
	slices.Sort(stems)
	if want := []string{"PA", "PB"}; !slices.Equal(stems, want) {
		t.Errorf("extracted stems = %v, want %v (staged as KEY.pdf)", stems, want)
	}

	// PA and PB have complete layout dirs; PC's pre-built dir survives.
	for _, k := range []string{"PA", "PB", "PC"} {
		if !layout.Done(k) {
			t.Errorf("layout %s not Done", k)
		}
	}
	if _, err := os.Stat(filepath.Join(layout.KeyDir("PA"), "tables", "table-001.csv")); err != nil {
		t.Errorf("PA tables missing: %v", err)
	}

	// Notes: PA (fresh) + PC (from layout md). PB skipped.
	if len(w.created) != 2 {
		t.Fatalf("created notes = %+v, want 2 (PA + PC)", w.created)
	}
	bodies := map[string]string{}
	for _, c := range w.created {
		bodies[c.parent] = c.body
	}
	if !strings.Contains(bodies["PA"], "# fresh") {
		t.Errorf("PA note body not from fresh extraction: %q", bodies["PA"])
	}
	if !strings.Contains(bodies["PC"], "# Title") {
		t.Errorf("PC note body not from layout markdown: %q", bodies["PC"])
	}

	// Outcome bookkeeping: layout written for PA + PB, PC served from disk.
	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if !byKey["PA"].LayoutWritten || !byKey["PB"].LayoutWritten {
		t.Errorf("LayoutWritten: PA=%v PB=%v, want true/true", byKey["PA"].LayoutWritten, byKey["PB"].LayoutWritten)
	}
	if byKey["PC"].LayoutWritten {
		t.Error("PC re-wrote a Done layout")
	}
	if !byKey["PC"].FromCache {
		t.Error("PC not marked FromCache (served from layout)")
	}
}

// TestExecuteBatch_LayoutNotePostRetry: a failed note post after a
// successful extraction must not cost a re-extract — the layout dir is
// Done, so the retry run goes straight to posting.
func TestExecuteBatch_LayoutNotePostRetry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := &KeyLayout{Dir: filepath.Join(dir, "extracts")}
	pdf := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdf, "a")
	mkItems := func() []BatchItem {
		return []BatchItem{mkBatchItem("PA", "PDFA", "a.pdf", pdf, "ha", ActionCreate)}
	}
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	now := func() time.Time { return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) }

	// Run 1: extraction succeeds, posting fails.
	ex := &fakeExtractor{md: "# body\n", version: "docling 2.86.0", full: true}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     mkItems(),
		Extractor: ex,
		Writer:    &fakeNoteWriter{createErr: errors.New("zotero 500")},
		Cache:     cache,
		Layout:    layout,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err == nil {
		t.Fatal("run 1: expected posting error")
	}
	if !layout.Done("PA") {
		t.Fatal("run 1: layout not Done despite successful extraction")
	}

	// Run 2: no extraction, note posted.
	w := &fakeNoteWriter{}
	res, err = ExecuteBatch(context.Background(), BatchInput{
		Items:     mkItems(),
		Extractor: ex,
		Writer:    w,
		Cache:     cache,
		Layout:    layout,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err != nil {
		t.Fatalf("run 2: %v", res.Outcomes[0].Err)
	}
	if n := atomic.LoadInt32(&ex.calls); n != 1 {
		t.Errorf("extractor ran again on retry (calls = %d, want 1)", n)
	}
	if len(w.created) != 1 || w.created[0].parent != "PA" {
		t.Errorf("run 2 notes = %+v, want one for PA", w.created)
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
	created, _, _, failed, _ := res.Counts()
	if created != 1 || failed != 1 {
		t.Errorf("created=%d failed=%d; want 1/1", created, failed)
	}
}

// tooLongErr mimics api.WriteFailedError for Zotero's note-length
// rejection: an error whose NoteTooLong() reports the permanent verdict.
// A local type on purpose — extract can't import api (cycle via zot),
// so it detects the behavior, not the concrete type.
type tooLongErr struct{}

func (tooLongErr) Error() string     { return "batch item 0 failed: Note '<h1>x</h1>...' too long" }
func (tooLongErr) NoteTooLong() bool { return true }

// TestExecuteBatch_NoteTooLongMarksAndStopsRetrying is the retry-loop
// fix end to end: a note Zotero rejects for length fails ONCE (with a
// durable marker recorded), and the next run skips posting it entirely
// instead of burning another attempt.
func TestExecuteBatch_NoteTooLongMarksAndStopsRetrying(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	now := func() time.Time { return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC) }

	// Run 1: extraction succeeds, the post is rejected as too long.
	items := []BatchItem{mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate)}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: &fakeExtractor{md: "# huge\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{createErr: tooLongErr{}},
		Cache:     cache,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err == nil {
		t.Fatal("run 1: expected a failed outcome for the rejected note")
	}
	if !res.Outcomes[0].TooLong {
		t.Error("run 1: outcome must be flagged TooLong")
	}
	if !cache.TooLong("PDFA", "ha") {
		t.Error("run 1: too-long marker must be recorded in the cache")
	}
	if _, _, _, failed, _ := res.Counts(); failed != 1 {
		t.Errorf("run 1: failed = %d, want 1", failed)
	}

	// Run 2: same cache. The item is served from cache, but the recorded
	// verdict means the writer must never be called again.
	w2 := &fakeNoteWriter{}
	res2, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     []BatchItem{mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate)},
		Extractor: &fakeExtractor{md: "# huge\n", version: "docling 2.86.0"},
		Writer:    w2,
		Cache:     cache,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(w2.created) != 0 {
		t.Errorf("run 2: CreateChildNote called %d time(s), want 0", len(w2.created))
	}
	o := res2.Outcomes[0]
	if o.Err != nil {
		t.Errorf("run 2: skipping a known-too-long note is not a failure, got %v", o.Err)
	}
	if !o.TooLong {
		t.Error("run 2: outcome must be flagged TooLong")
	}
	if o.NoteKey != "" {
		t.Errorf("run 2: no note must be created, got key %q", o.NoteKey)
	}
	created, _, _, failed, tooLong := res2.Counts()
	if created != 0 || failed != 0 || tooLong != 1 {
		t.Errorf("run 2: created=%d failed=%d tooLong=%d; want 0/0/1", created, failed, tooLong)
	}
}

// TestExecuteBatch_OtherPostErrorsStayRetriable: a transient post
// failure (not a length rejection) must NOT be recorded as permanent —
// the next run retries it.
func TestExecuteBatch_OtherPostErrorsStayRetriable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdfA := filepath.Join(dir, "a.pdf")
	writeStubPDF(t, pdfA, "a")
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     []BatchItem{mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate)},
		Extractor: &fakeExtractor{md: "# h\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{createErr: errors.New("503 service unavailable")},
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0].Err == nil {
		t.Fatal("expected failed outcome")
	}
	if res.Outcomes[0].TooLong {
		t.Error("transient failure must not be flagged TooLong")
	}
	if cache.TooLong("PDFA", "ha") {
		t.Error("transient failure must not record a permanent marker")
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
	for i := range N {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("b%d", i))
		items[i] = mkBatchItem(
			fmt.Sprintf("P%d", i),
			fmt.Sprintf("PDF%d", i),
			fmt.Sprintf("p%d.pdf", i),
			p,
			fmt.Sprintf("h%d", i),
			ActionCreate,
		)
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
		mkBatchItem("P", "PDF1", "p.pdf", pdf, "h", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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

// TestExecuteBatch_ChunkedQueue: 6 equal-cost items pool into chunks
// of chunkTargetDocs (5) → 2 ExtractBatch invocations of 5 and 1,
// drained by the worker queue.
func TestExecuteBatch_ChunkedQueue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 6
	items := make([]BatchItem, N)
	for i := range N {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("body%d", i))
		items[i] = mkBatchItem(
			fmt.Sprintf("P%d", i),
			fmt.Sprintf("PDF%d", i),
			fmt.Sprintf("p%d.pdf", i),
			p,
			fmt.Sprintf("h%d", i),
			ActionCreate,
		)
	}

	ex := &fakeExtractor{md: "# chunk\n", version: "docling 2.86.0"}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:         items,
		Extractor:     ex,
		Writer:        w,
		Cache:         &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Jobs:          3,
		PageEstimator: func(string) int { return 10 },
		Now:           func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// 6 equal-cost items at target 5 → chunks of [5, 1] → 2 calls.
	if atomic.LoadInt32(&ex.calls) != 2 {
		t.Errorf("extractor calls = %d, want 2 (chunks of 5+1)", atomic.LoadInt32(&ex.calls))
	}
	sizes := lo.Map(ex.batches, func(b []string, _ int) int { return len(b) })
	slices.Sort(sizes)
	if !slices.Equal(sizes, []int{1, 5}) {
		t.Errorf("chunk sizes = %v, want [1 5]", sizes)
	}
	created, _, _, failed, _ := res.Counts()
	if created != 6 || failed != 0 {
		t.Errorf("created=%d failed=%d; want 6/0", created, failed)
	}
	if len(w.created) != 6 {
		t.Errorf("notes posted = %d, want 6", len(w.created))
	}
}

// TestExecuteBatch_UnderTargetIsOneChunk: fewer items than
// chunkTargetDocs → one ExtractBatch call regardless of Jobs=0.
func TestExecuteBatch_UnderTargetIsOneChunk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 4
	items := make([]BatchItem, N)
	for i := range N {
		p := filepath.Join(dir, fmt.Sprintf("p%d.pdf", i))
		writeStubPDF(t, p, fmt.Sprintf("body%d", i))
		items[i] = mkBatchItem(
			fmt.Sprintf("P%d", i),
			fmt.Sprintf("PDF%d", i),
			fmt.Sprintf("p%d.pdf", i),
			p,
			fmt.Sprintf("h%d", i),
			ActionCreate,
		)
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

// TestResolveDevice: "auto" (and empty) resolve to the platform's real
// accelerator so jobs/ETA pricing sees a concrete device; explicit
// choices pass through untouched.
func TestResolveDevice(t *testing.T) {
	t.Parallel()
	for _, explicit := range []string{"mps", "cpu", "cuda"} {
		if got := ResolveDevice(explicit); got != explicit {
			t.Errorf("ResolveDevice(%q) = %q, want passthrough", explicit, got)
		}
	}
	want := "cpu"
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		want = "mps"
	}
	if got := ResolveDevice("auto"); got != want {
		t.Errorf("ResolveDevice(auto) = %q, want %q on %s/%s", got, want, runtime.GOOS, runtime.GOARCH)
	}
	if got := ResolveDevice(""); got != want {
		t.Errorf("ResolveDevice(\"\") = %q, want %q (empty means auto)", got, want)
	}
}

// TestBatchJobsDefault: mps defaults to 2 workers (unified memory,
// CPU-bound OCR); cuda stays 1 (VRAM-bound); auto resolves to the
// platform device first (mps on Apple silicon), so the flag's "auto"
// default doesn't silently halve bulk throughput.
func TestBatchJobsDefault(t *testing.T) {
	t.Parallel()
	if got := BatchJobsDefault("mps", 8); got != 2 {
		t.Errorf("mps, 8CPU → %d, want 2", got)
	}
	if got := BatchJobsDefault("cuda", 16); got != 1 {
		t.Errorf("cuda, 16CPU → %d, want 1", got)
	}
	if got, want := BatchJobsDefault("auto", 8), BatchJobsDefault(ResolveDevice("auto"), 8); got != want {
		t.Errorf("auto, 8CPU → %d, want %d (resolved-device default)", got, want)
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if got := BatchJobsDefault("auto", 8); got != 2 {
			t.Errorf("auto on Apple silicon, 8CPU → %d, want 2 (mps default)", got)
		}
	}
	if got := BatchJobsDefault("cpu", 8); got != 2 {
		t.Errorf("cpu, 8CPU → %d, want 2", got)
	}
	if got := BatchJobsDefault("cpu", 2); got != 1 {
		t.Errorf("cpu, 2CPU → %d, want 1 (floor)", got)
	}
}

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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
		mkBatchItem("PB", "PDFB", "b.pdf", pdfB, "hb", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
		mkBatchItem("PB", "PDFB", "b.pdf", pdfB, "hb", ActionCreate),
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

	// Verify phase order (Estimate precedes Extract).
	if len(phases) != 4 {
		t.Fatalf("got %d phases, want 4; phases: %+v", len(phases), phases)
	}
	wantPhases := []phaseEvent{
		{PhasePostCached, 1},
		{PhaseEstimate, 1},
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
	created, _, _, failed, _ := res.Counts()
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
	created, _, _, _, _ := res.Counts()
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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

	// Estimate + Extract + PostFresh, no PostCached.
	if len(phases) != 3 {
		t.Fatalf("got %d phases, want 3; phases: %+v", len(phases), phases)
	}
	if phases[0].phase != PhaseEstimate {
		t.Errorf("phase[0] = %+v, want Estimate", phases[0])
	}
	if phases[1].phase != PhaseExtract {
		t.Errorf("phase[1] = %+v, want Extract", phases[1])
	}
	if phases[2].phase != PhasePostFresh {
		t.Errorf("phase[2] = %+v, want PostFresh", phases[2])
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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
		mkBatchItem("PA", "PDFA", "a.pdf", pdfA, "ha", ActionCreate),
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

// TestExecuteBatch_ClassicModeNoSalvageOnChunkError locks classic
// (note-only) mode's behavior byte-identical against the layout-mode
// salvage: a chunk error still fails every item in the chunk, and the
// markdown cache — classic mode's own crash-resume store — keeps what
// the in-flight drain banked before the death.
func TestExecuteBatch_ClassicModeNoSalvageOnChunkError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var items []BatchItem
	for _, k := range []string{"k1", "k2", "k3"} {
		pdf := filepath.Join(dir, k+".pdf")
		writeStubPDF(t, pdf, k)
		items = append(items, mkBatchItem("P"+k, "PDF"+k, k+".pdf", pdf, "h"+k, ActionCreate))
	}
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"k1": {writeMD: true, secs: 1},
			"k2": {writeMD: true, secs: 1},
			"k3": {writeMD: true, secs: 1},
		},
		hook: func(p hookPoint, stem string) error {
			// Abort after k1's markdown has been drained into the cache
			// (the drain runs on k2's Processing event, before this hook).
			if p == afterDrain && stem == "k1" {
				return errors.New("docling exit: signal: killed")
			}
			return nil
		},
	}
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	res, err := ExecuteBatch(context.Background(), BatchInput{
		Items:     items,
		Extractor: ex,
		Writer:    &fakeNoteWriter{},
		Cache:     cache,
		Now:       func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Classic mode: the whole chunk fails — no per-item salvage.
	for i, o := range res.Outcomes {
		if o.Err == nil {
			t.Errorf("outcome %d has no error; classic mode must fail the chunk wholesale", i)
		}
	}
	// But the incremental cache drain already banked k1 for resume.
	if _, ok := cache.Get("PDFk1", "hk1"); !ok {
		t.Error("cache missing k1 — the classic drain regressed")
	}
}
