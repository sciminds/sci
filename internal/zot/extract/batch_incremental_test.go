package extract

// Tests for incremental per-document layout finalization: documents are
// banked into the per-key layout as docling completes them (not when the
// whole chunk's process exits), an errored/canceled chunk salvages every
// completed document from its job dir, and only the genuinely unfinished
// tail fails. The scripted extractor below faithfully models docling's
// event/write ordering — most importantly that "Finished converting" is
// logged BEFORE the output files exist, which is why finalization queues
// on EventFinished and completes on a later drain.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// docStep scripts one document inside a scriptedExtractor batch.
type docStep struct {
	writeMD   bool
	writeJSON bool    // also writes <stem>_artifacts/image_000000.png
	secs      float64 // reported on EventFinished
	fail      bool    // emit EventFailed, write nothing
}

// hookPoint identifies where in a scripted document's lifecycle the
// extractor calls back into the test.
type hookPoint int

const (
	// afterOutputs fires the instant a document's files are on disk with
	// no further docling event emitted — the state a chunk is in when
	// docling is killed immediately after finishing a document.
	afterOutputs hookPoint = iota
	// afterDrain fires once one more event has been delivered (the next
	// document's EventProcessing, or the batch summary), which is the
	// earliest point the batch layer can have finalized the stem.
	afterDrain
)

// scriptedExtractor replays a docling batch with per-document control.
// Unlike fakeExtractor (which writes everything before announcing it),
// this models the real interleaving: EventFinished before the files
// exist, EventOutput before each write, and a hook between documents so
// tests can observe or abort mid-batch. Holds no mutable state, so
// parallel chunks are race-clean.
type scriptedExtractor struct {
	steps     map[string]docStep // keyed by stem; read-only after construction
	md        string             // markdown body written for writeMD steps
	version   string
	batchDur  time.Duration // BatchExtractResult.Duration — the whole-batch total time
	noSummary bool          // suppress EventSummary (last-doc-with-no-drain case)
	// hook, when non-nil, is called at both hook points for every
	// document. A non-nil return aborts the batch with that error.
	hook func(p hookPoint, stem string) error
}

func (s *scriptedExtractor) Extract(context.Context, ExtractOptions) (*ExtractResult, error) {
	return nil, errors.New("scriptedExtractor: single-document Extract is not scripted")
}

func (s *scriptedExtractor) ExtractBatch(_ context.Context, opts ExtractOptions, pdfs []string, onProgress ProgressFunc) (*BatchExtractResult, error) {
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}
	emit := func(ev *DoclingEvent) {
		if onProgress != nil {
			onProgress(ev)
		}
	}
	callHook := func(p hookPoint, stem string) error {
		if s.hook == nil {
			return nil
		}
		return s.hook(p, stem)
	}

	results := map[string]*ExtractResult{}
	var failed []string
	prev := ""
	processed := 0
	for _, pdf := range pdfs {
		stem := stemFor(pdf)
		emit(&DoclingEvent{Kind: EventProcessing, Document: stem + ".pdf"})
		if prev != "" {
			if err := callHook(afterDrain, prev); err != nil {
				return nil, err
			}
		}
		step := s.steps[stem]
		if step.fail {
			emit(&DoclingEvent{Kind: EventFailed, Document: pdf})
			failed = append(failed, pdf)
			prev = stem
			continue
		}
		// Docling logs "Finished converting" before any file exists.
		emit(&DoclingEvent{
			Kind:     EventFinished,
			Document: stem + ".pdf",
			Duration: time.Duration(step.secs * float64(time.Second)),
		})
		mdPath := filepath.Join(opts.OutputDir, stem+".md")
		if step.writeMD {
			// The output path is logged before the file is written.
			emit(&DoclingEvent{Kind: EventOutput, OutputPath: mdPath})
			if err := os.WriteFile(mdPath, []byte(s.md), 0o644); err != nil {
				return nil, err
			}
		}
		if step.writeJSON {
			jsonPath := filepath.Join(opts.OutputDir, stem+".json")
			emit(&DoclingEvent{Kind: EventOutput, OutputPath: jsonPath})
			if err := os.WriteFile(jsonPath, []byte(fakeDoclingJSON), 0o644); err != nil {
				return nil, err
			}
			artDir := filepath.Join(opts.OutputDir, stem+"_artifacts")
			if err := os.MkdirAll(artDir, 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(artDir, "image_000000.png"), []byte("png"), 0o644); err != nil {
				return nil, err
			}
		}
		if err := callHook(afterOutputs, stem); err != nil {
			return nil, err
		}
		if step.writeMD {
			results[pdf] = &ExtractResult{MarkdownPath: mdPath, ToolVersion: s.version, Duration: s.batchDur}
		}
		processed++
		prev = stem
	}
	if !s.noSummary {
		emit(&DoclingEvent{Kind: EventSummary, Processed: processed, Failed: len(failed)})
		if prev != "" {
			if err := callHook(afterDrain, prev); err != nil {
				return nil, err
			}
		}
	}
	return &BatchExtractResult{
		Results:     results,
		FailedDocs:  failed,
		ToolVersion: s.version,
		Duration:    s.batchDur,
	}, nil
}

// layoutFixture is the shared scaffolding for the incremental tests:
// a layout root, a markdown cache, and one stub PDF + BatchItem per key.
type layoutFixture struct {
	dir    string
	layout *KeyLayout
	cache  *MarkdownCache
	items  []BatchItem
}

func newLayoutFixture(t *testing.T, keys []string, action Action) *layoutFixture {
	t.Helper()
	dir := t.TempDir()
	f := &layoutFixture{
		dir:    dir,
		layout: &KeyLayout{Dir: filepath.Join(dir, "extracts")},
		cache:  &MarkdownCache{Dir: filepath.Join(dir, "cache")},
	}
	for _, k := range keys {
		pdf := filepath.Join(dir, strings.ToLower(k)+".pdf")
		writeStubPDF(t, pdf, k)
		f.items = append(f.items, mkBatchItem(k, "PDF"+k, strings.ToLower(k)+".pdf", pdf, "h"+k, action))
	}
	return f
}

func (f *layoutFixture) input(ex Extractor, w NoteWriter) BatchInput {
	return BatchInput{
		Items:     f.items,
		Extractor: ex,
		Writer:    w,
		Cache:     f.cache,
		Layout:    f.layout,
		Now:       func() time.Time { return time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) },
	}
}

// readManifest unmarshals <layout>/<key>/result.json.
func readManifest(t *testing.T, layout *KeyLayout, key string) LayoutManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(layout.KeyDir(key), "result.json"))
	if err != nil {
		t.Fatalf("read %s manifest: %v", key, err)
	}
	var man LayoutManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatalf("parse %s manifest: %v", key, err)
	}
	return man
}

// TestExecuteBatch_LayoutFinalizesDuringExtraction: each document's
// layout dir is Done by the time the NEXT document's processing event
// has been delivered — finalization happens during extraction, not
// after the process exits.
func TestExecuteBatch_LayoutFinalizesDuringExtraction(t *testing.T) {
	t.Parallel()
	keys := []string{"K1", "K2", "K3"}
	f := newLayoutFixture(t, keys, ActionCreate)

	type snapshot struct {
		stem string
		done map[string]bool
	}
	var mu sync.Mutex
	var snaps []snapshot

	ex := &scriptedExtractor{
		md:      "# body\n",
		version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1},
			"K2": {writeMD: true, writeJSON: true, secs: 1},
			"K3": {writeMD: true, writeJSON: true, secs: 1},
		},
		hook: func(p hookPoint, stem string) error {
			if p != afterDrain {
				return nil
			}
			done := map[string]bool{}
			for _, k := range keys {
				done[k] = f.layout.Done(k)
			}
			mu.Lock()
			snaps = append(snaps, snapshot{stem: stem, done: done})
			mu.Unlock()
			return nil
		},
	}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), f.input(ex, w))
	if err != nil {
		t.Fatal(err)
	}

	// At each drain point the drained stem is Done and every later stem
	// is not.
	mu.Lock()
	defer mu.Unlock()
	if len(snaps) != 3 {
		t.Fatalf("got %d drain snapshots, want 3", len(snaps))
	}
	for i, snap := range snaps {
		if !snap.done[snap.stem] {
			t.Errorf("drain %d: %s not Done at its own drain point", i, snap.stem)
		}
		for _, later := range keys[i+1:] {
			if snap.done[later] {
				t.Errorf("drain %d (%s): later stem %s already Done", i, snap.stem, later)
			}
		}
	}

	for _, k := range keys {
		if !f.layout.Done(k) {
			t.Errorf("%s not Done after run", k)
		}
	}
	if len(w.created) != 3 {
		t.Errorf("created notes = %d, want 3", len(w.created))
	}
	for _, o := range res.Outcomes {
		if !o.LayoutWritten {
			t.Errorf("%s: LayoutWritten = false", o.Item.Request.ParentKey)
		}
	}
}

// TestExecuteBatch_LayoutManifestUsesPerDocDuration: result.json carries
// the document's own EventFinished duration, never the batch total.
func TestExecuteBatch_LayoutManifestUsesPerDocDuration(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1", "K2"}, ActionCreate)
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		batchDur: 99 * time.Second, // the lie the old code recorded
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1.5},
			"K2": {writeMD: true, writeJSON: true, secs: 42.25},
		},
	}
	res, err := ExecuteBatch(context.Background(), f.input(ex, &fakeNoteWriter{}))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]float64{"K1": 1.5, "K2": 42.25}
	for key, secs := range want {
		man := readManifest(t, f.layout, key)
		if man.Secs != secs {
			t.Errorf("%s manifest secs = %v, want %v", key, man.Secs, secs)
		}
		if man.Secs == 99 {
			t.Errorf("%s manifest carries the batch total", key)
		}
	}
	for _, o := range res.Outcomes {
		wantDur := time.Duration(want[o.Item.Request.ParentKey] * float64(time.Second))
		if o.Duration != wantDur {
			t.Errorf("%s outcome duration = %v, want %v", o.Item.Request.ParentKey, o.Duration, wantDur)
		}
	}
}

// TestExecuteBatch_LayoutFinalizesLastDocAfterExit: with no event after
// the last document's outputs (no summary line), only the post-exit
// sweep can finalize it. Guards that the sweep wasn't dropped when the
// incremental drain was added.
func TestExecuteBatch_LayoutFinalizesLastDocAfterExit(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1"}, ActionCreate)
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9", noSummary: true,
		steps: map[string]docStep{"K1": {writeMD: true, writeJSON: true, secs: 7.5}},
	}
	w := &fakeNoteWriter{}
	if _, err := ExecuteBatch(context.Background(), f.input(ex, w)); err != nil {
		t.Fatal(err)
	}
	if !f.layout.Done("K1") {
		t.Fatal("K1 not Done — post-exit sweep missing")
	}
	if len(w.created) != 1 {
		t.Errorf("created notes = %d, want 1", len(w.created))
	}
	if man := readManifest(t, f.layout, "K1"); man.Secs != 7.5 {
		t.Errorf("manifest secs = %v, want 7.5 (per-doc duration must survive the sweep)", man.Secs)
	}
}

// TestExecuteBatch_LayoutFinalizesOncePerStem: the drain finalizes both
// documents during the run, then the post-exit sweep runs over the same
// stems — a second Finalize would destroy a completed dir (its first
// act is clearing it), so each stem must finalize exactly once.
func TestExecuteBatch_LayoutFinalizesOncePerStem(t *testing.T) {
	t.Parallel()
	// ActionSkip so OnItemDone fires only from finalize, never postNote.
	f := newLayoutFixture(t, []string{"K1", "K2"}, ActionSkip)
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 3},
			"K2": {writeMD: true, writeJSON: true, secs: 4},
		},
	}
	var mu sync.Mutex
	var doneIdx []int
	in := f.input(ex, &fakeNoteWriter{})
	in.OnItemDone = func(i int, _ BatchOutcome) {
		mu.Lock()
		doneIdx = append(doneIdx, i)
		mu.Unlock()
	}
	if _, err := ExecuteBatch(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	slices.Sort(doneIdx)
	if !slices.Equal(doneIdx, []int{0, 1}) {
		t.Errorf("OnItemDone indices = %v, want exactly [0 1]", doneIdx)
	}
	if man := readManifest(t, f.layout, "K1"); man.Secs != 3 {
		t.Errorf("K1 secs = %v, want 3 (a re-finalize would have zeroed it)", man.Secs)
	}
	if man := readManifest(t, f.layout, "K2"); man.Secs != 4 {
		t.Errorf("K2 secs = %v, want 4", man.Secs)
	}
}

// TestExecuteBatch_LayoutFailedDocIsNotFinalized: a document that fails
// under --no-abort-on-error produces no outputs; its siblings finalize
// normally and it alone carries an error.
func TestExecuteBatch_LayoutFailedDocIsNotFinalized(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1", "K2", "K3"}, ActionCreate)
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1},
			"K2": {fail: true},
			"K3": {writeMD: true, writeJSON: true, secs: 1},
		},
	}
	res, err := ExecuteBatch(context.Background(), f.input(ex, &fakeNoteWriter{}))
	if err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"K1", "K3"} {
		if !f.layout.Done(k) {
			t.Errorf("%s not Done", k)
		}
	}
	if f.layout.Done("K2") {
		t.Error("failed K2 is Done")
	}
	if _, err := os.Stat(f.layout.KeyDir("K2")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("K2 key dir exists: %v", err)
	}
	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if byKey["K1"].Err != nil || byKey["K3"].Err != nil {
		t.Errorf("sibling errors: K1=%v K3=%v", byKey["K1"].Err, byKey["K3"].Err)
	}
	if byKey["K2"].Err == nil || !strings.Contains(byKey["K2"].Err.Error(), "docling produced no output") {
		t.Errorf("K2 err = %v, want 'docling produced no output'", byKey["K2"].Err)
	}
}

// TestExecuteBatch_LayoutSalvagesCompletedDocsOnChunkError: when the
// docling process dies mid-batch, every document whose outputs are on
// disk is finalized anyway — K1 via the drain, K2 via the sweep (its
// outputs exist but no event followed them) — and only the never-run
// tail fails.
func TestExecuteBatch_LayoutSalvagesCompletedDocsOnChunkError(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1", "K2", "K3"}, ActionCreate)
	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1},
			"K2": {writeMD: true, writeJSON: true, secs: 2},
			"K3": {writeMD: true, writeJSON: true, secs: 3},
		},
		hook: func(p hookPoint, stem string) error {
			if p == afterOutputs && stem == "K2" {
				return errors.New("extractbatch: docling exit: signal: killed")
			}
			return nil
		},
	}
	w := &fakeNoteWriter{}
	res, err := ExecuteBatch(context.Background(), f.input(ex, w))
	if err != nil {
		t.Fatal(err)
	}

	if !f.layout.Done("K1") {
		t.Error("K1 not Done (drain path)")
	}
	if !f.layout.Done("K2") {
		t.Error("K2 not Done — the sweep must salvage outputs that never got a drain event")
	}
	if f.layout.Done("K3") {
		t.Error("K3 Done but was never extracted")
	}

	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if byKey["K1"].Err != nil || byKey["K2"].Err != nil {
		t.Errorf("salvaged docs carry errors: K1=%v K2=%v", byKey["K1"].Err, byKey["K2"].Err)
	}
	if byKey["K3"].Err == nil || !strings.Contains(byKey["K3"].Err.Error(), "signal: killed") {
		t.Errorf("K3 err = %v, want the chunk error", byKey["K3"].Err)
	}
	if _, _, _, failed := res.Counts(); failed != 1 {
		t.Errorf("failed = %d, want 1 (not the whole chunk)", failed)
	}
	if len(w.created) != 2 {
		t.Errorf("created notes = %d, want 2 (K1 + K2)", len(w.created))
	}
	// The chunk died before reporting a version; salvaged notes must not
	// post with an empty source.
	if res.ToolVersion != "docling" {
		t.Errorf("ToolVersion = %q, want the %q fallback", res.ToolVersion, "docling")
	}
}

// TestExecuteBatch_LayoutSalvageSkipsHalfWrittenDoc: a document with
// markdown but no JSON is not complete — the sweep must not feed it to
// Finalize, and any pre-existing (non-Done) partial dir for that key
// must survive untouched.
func TestExecuteBatch_LayoutSalvageSkipsHalfWrittenDoc(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1", "K2"}, ActionCreate)

	// Pre-seed a partial K2 dir (no .done marker, so it still extracts).
	// If the sweep ever called Finalize on the half-written doc, this
	// dir would be cleared.
	relic := filepath.Join(f.layout.KeyDir("K2"), "relic.txt")
	if err := os.MkdirAll(f.layout.KeyDir("K2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(relic, []byte("survive me"), 0o644); err != nil {
		t.Fatal(err)
	}

	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1},
			"K2": {writeMD: true, writeJSON: false, secs: 2}, // half-written
		},
		hook: func(p hookPoint, stem string) error {
			if p == afterOutputs && stem == "K2" {
				return errors.New("docling exit: signal: killed")
			}
			return nil
		},
	}
	res, err := ExecuteBatch(context.Background(), f.input(ex, &fakeNoteWriter{}))
	if err != nil {
		t.Fatal(err)
	}

	if !f.layout.Done("K1") {
		t.Error("K1 not Done")
	}
	if f.layout.Done("K2") {
		t.Error("half-written K2 is Done")
	}
	if body, err := os.ReadFile(relic); err != nil || string(body) != "survive me" {
		t.Errorf("pre-existing K2 dir was touched: body=%q err=%v", body, err)
	}
	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if byKey["K2"].Err == nil {
		t.Error("half-written K2 has no error")
	}
}

// TestExecuteBatch_LayoutCancelDuringExtractionBanksFinishedWork: the
// headline regression. Interrupting a run keeps every finished
// document's layout dir — including after ExecuteBatch has returned and
// the deferred temp-dir cleanup has run.
func TestExecuteBatch_LayoutCancelDuringExtractionBanksFinishedWork(t *testing.T) {
	t.Parallel()
	f := newLayoutFixture(t, []string{"K1", "K2", "K3"}, ActionCreate)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ex := &scriptedExtractor{
		md: "# body\n", version: "docling 9.9.9",
		steps: map[string]docStep{
			"K1": {writeMD: true, writeJSON: true, secs: 1},
			"K2": {writeMD: true, writeJSON: true, secs: 2},
			"K3": {writeMD: true, writeJSON: true, secs: 3},
		},
		hook: func(p hookPoint, stem string) error {
			if p == afterDrain && stem == "K1" {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}
	res, err := ExecuteBatch(ctx, f.input(ex, &fakeNoteWriter{}))
	if err != nil {
		t.Fatalf("ExecuteBatch must return per-item errors, not fail wholesale: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	// K1's banked layout survives ExecuteBatch's deferred temp cleanup.
	for _, p := range []string{
		f.layout.MarkdownPath("K1"),
		f.layout.JSONPath("K1"),
		filepath.Join(f.layout.KeyDir("K1"), "result.json"),
		filepath.Join(f.layout.KeyDir("K1"), ".done"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("banked K1 file missing after return: %v", err)
		}
	}
	byKey := map[string]BatchOutcome{}
	for _, o := range res.Outcomes {
		byKey[o.Item.Request.ParentKey] = o
	}
	if byKey["K1"].Err != nil {
		t.Errorf("K1 err = %v, want banked success", byKey["K1"].Err)
	}
	for _, k := range []string{"K2", "K3"} {
		if f.layout.Done(k) {
			t.Errorf("%s Done but never extracted", k)
		}
		if byKey[k].Err == nil {
			t.Errorf("%s has no error after cancel", k)
		}
	}
}

// TestExecuteBatch_LayoutParallelChunksFinalizeIndependently: with
// Jobs>1 each chunk's finalizer owns disjoint stems and outcome
// indices. Run under -race — this is the proof the callback-goroutine
// writes don't contend.
func TestExecuteBatch_LayoutParallelChunksFinalizeIndependently(t *testing.T) {
	t.Parallel()
	keys := []string{"K1", "K2", "K3", "K4", "K5", "K6"}
	// ActionSkip: OnItemDone fires from finalize on the chunk goroutines.
	f := newLayoutFixture(t, keys, ActionSkip)

	steps := map[string]docStep{}
	for i, k := range keys {
		steps[k] = docStep{writeMD: true, writeJSON: true, secs: float64(i + 1)}
	}
	ex := &scriptedExtractor{md: "# body\n", version: "docling 9.9.9", steps: steps}

	var mu sync.Mutex
	var doneIdx []int
	in := f.input(ex, &fakeNoteWriter{})
	in.Jobs = 3
	in.OnItemDone = func(i int, _ BatchOutcome) {
		mu.Lock()
		doneIdx = append(doneIdx, i)
		mu.Unlock()
	}
	res, err := ExecuteBatch(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	for i, k := range keys {
		if !f.layout.Done(k) {
			t.Errorf("%s not Done", k)
		}
		if !res.Outcomes[i].LayoutWritten {
			t.Errorf("%s LayoutWritten = false", k)
		}
		if res.Outcomes[i].Err != nil {
			t.Errorf("%s err = %v", k, res.Outcomes[i].Err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(doneIdx)
	if !slices.Equal(doneIdx, []int{0, 1, 2, 3, 4, 5}) {
		t.Errorf("OnItemDone indices = %v, want each exactly once", doneIdx)
	}
}
