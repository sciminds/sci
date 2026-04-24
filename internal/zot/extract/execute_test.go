package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeExtractor writes a fixed markdown blob to opts.OutputDir and
// returns a synthetic ExtractResult. No docling dependency.
// Thread-safe: calls is incremented atomically since ExecuteBatch
// may invoke ExtractBatch from multiple goroutines (parallel jobs).
type fakeExtractor struct {
	md      string
	version string
	err     error
	calls   int32 // use atomic ops
}

func (f *fakeExtractor) Extract(_ context.Context, opts ExtractOptions) (*ExtractResult, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return nil, f.err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}
	stem := stemFor(opts.PDFPath)
	mdPath := filepath.Join(opts.OutputDir, stem+".md")
	if err := os.WriteFile(mdPath, []byte(f.md), 0o644); err != nil {
		return nil, err
	}
	return &ExtractResult{
		MarkdownPath: mdPath,
		ToolVersion:  f.version,
		Duration:     time.Second,
	}, nil
}

func (f *fakeExtractor) ExtractBatch(_ context.Context, opts ExtractOptions, pdfs []string, onProgress ProgressFunc) (*BatchExtractResult, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return nil, f.err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}
	results := make(map[string]*ExtractResult, len(pdfs))
	for _, pdf := range pdfs {
		stem := stemFor(pdf)
		mdPath := filepath.Join(opts.OutputDir, stem+".md")
		if err := os.WriteFile(mdPath, []byte(f.md), 0o644); err != nil {
			return nil, err
		}
		results[pdf] = &ExtractResult{
			MarkdownPath: mdPath,
			ToolVersion:  f.version,
			Duration:     time.Second,
		}
		if onProgress != nil {
			onProgress(&DoclingEvent{Kind: EventFinished, Document: stem + ".pdf"})
			onProgress(&DoclingEvent{Kind: EventOutput, OutputPath: mdPath})
		}
	}
	return &BatchExtractResult{
		Results:     results,
		ToolVersion: f.version,
		Duration:    time.Duration(len(pdfs)) * time.Second,
	}, nil
}

// fakeNoteWriter records every CreateChildNote and AddTagToItem call.
type fakeNoteWriter struct {
	created   []createCall
	tagged    []tagCall
	nextKey   int
	createErr error
	tagErr    error
}

type createCall struct {
	parent string
	body   string
	tags   []string
}

type tagCall struct {
	item string
	tag  string
}

func (f *fakeNoteWriter) CreateChildNote(_ context.Context, parent, body string, tags []string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.nextKey++
	key := fmt.Sprintf("NOTE%04d", f.nextKey)
	f.created = append(f.created, createCall{parent: parent, body: body, tags: tags})
	return key, nil
}

func (f *fakeNoteWriter) AddTagToItem(_ context.Context, item, tag string) error {
	if f.tagErr != nil {
		return f.tagErr
	}
	f.tagged = append(f.tagged, tagCall{item: item, tag: tag})
	return nil
}

// baseInput builds a reusable ExecuteInput — each test copies and
// mutates fields as needed so one default shape keeps the tests short.
func baseInput(t *testing.T, plan *Plan) ExecuteInput {
	t.Helper()
	dir := t.TempDir()
	// Drop a stub PDF so stemFor picks up a clean name.
	pdf := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(pdf, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ExecuteInput{
		Plan:      plan,
		Extractor: &fakeExtractor{md: "## Heading\n\nBody.\n", version: "docling 2.86.0"},
		Writer:    &fakeNoteWriter{},
		PDFPath:   pdf,
		OutputDir: filepath.Join(dir, "out"),
		ExtractOpts: ExtractOptions{
			Formats: []OutputFormat{FormatMarkdown},
		},
		Now: func() time.Time { return time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC) },
	}
}

// TestExecute_Create: ActionCreate runs the extractor, renders the
// body, and posts via CreateChildNote with the default docling tag.
func TestExecute_Create(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{
			ParentKey: "PARENT01",
			PDFKey:    "PDF1",
			PDFName:   "paper.pdf",
			PDFHash:   "abc123",
		},
		Action: ActionCreate,
		Reason: "no existing docling note",
	}
	in := baseInput(t, plan)

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteKey == "" || !strings.HasPrefix(res.NoteKey, "NOTE") {
		t.Errorf("NoteKey = %q, want NOTE*", res.NoteKey)
	}
	if atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls) != 1 {
		t.Errorf("extractor calls = %d, want 1", atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls))
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 1 {
		t.Fatalf("CreateChildNote calls = %d, want 1", len(w.created))
	}
	call := w.created[0]
	if call.parent != "PARENT01" {
		t.Errorf("parent = %q", call.parent)
	}
	// Default tag applied.
	if len(call.tags) != 1 || call.tags[0] != "docling" {
		t.Errorf("tags = %v, want [docling]", call.tags)
	}
	// Result body should match what was posted.
	if res.Body != call.body {
		t.Error("result Body differs from posted body")
	}
}

// TestExecute_Skip: ActionSkip must short-circuit — no extractor run,
// no writer calls.
func TestExecute_Skip(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "PARENT01", PDFKey: "PDF1", PDFHash: "abc123"},
		Action:  ActionSkip,
		Reason:  "docling note already exists",
	}
	in := baseInput(t, plan)

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteKey != "" {
		t.Errorf("NoteKey = %q, want empty for skip", res.NoteKey)
	}
	if atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls) != 0 {
		t.Errorf("Skip must not invoke extractor; calls = %d", atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls))
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 0 {
		t.Errorf("Skip must not call writer; created=%d", len(w.created))
	}
	if res.Body != "" {
		t.Errorf("Skip should leave Body empty; got %q", res.Body)
	}
}

// TestExecute_DefaultIsMarkdown: the default (RenderHTML=false) stores
// raw markdown with YAML frontmatter in the note body.
func TestExecute_DefaultIsMarkdown(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)

	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	body := in.Writer.(*fakeNoteWriter).created[0].body
	if !strings.Contains(body, "---\n") {
		t.Errorf("body missing YAML frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "pdf_key: PDF1") {
		t.Errorf("body missing pdf_key in frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "## Heading") {
		t.Errorf("body missing raw markdown:\n%s", body)
	}
}

// TestExecute_RenderHTML: when RenderHTML is true, the posted body
// contains goldmark-rendered HTML instead of raw markdown.
func TestExecute_RenderHTML(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.RenderHTML = true

	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	body := in.Writer.(*fakeNoteWriter).created[0].body
	// goldmark renders ## Heading as <h2>
	if !strings.Contains(body, "<h2") {
		t.Errorf("body missing rendered <h2>:\n%s", body)
	}
	if strings.Contains(body, "## Heading") {
		t.Errorf("body should not contain raw markdown when RenderHTML=true:\n%s", body)
	}
}

// TestExecute_FrontmatterIncludesDOI: when DOI is set, it appears
// in the YAML frontmatter.
func TestExecute_FrontmatterIncludesDOI(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{
			ParentKey: "P",
			PDFKey:    "PDF1",
			PDFName:   "p.pdf",
			PDFHash:   "h",
			DOI:       "10.1234/test.2026",
		},
		Action: ActionCreate,
	}
	in := baseInput(t, plan)

	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	body := in.Writer.(*fakeNoteWriter).created[0].body
	if !strings.Contains(body, "doi: \"10.1234/test.2026\"") {
		t.Errorf("body missing DOI in frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "zotero_key: P") {
		t.Errorf("body missing zotero_key in frontmatter:\n%s", body)
	}
}

// TestExecute_FrontmatterOmitsDOIWhenEmpty: when DOI is empty,
// the doi field should not appear in frontmatter.
func TestExecute_FrontmatterOmitsDOIWhenEmpty(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)

	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	body := in.Writer.(*fakeNoteWriter).created[0].body
	if strings.Contains(body, "doi:") {
		t.Errorf("body should not contain doi when empty:\n%s", body)
	}
}

// TestExecute_CustomTags: explicit Tags override the default "docling".
func TestExecute_CustomTags(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.Tags = []string{"extract", "pdf2md"}
	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	got := in.Writer.(*fakeNoteWriter).created[0].tags
	if len(got) != 2 || got[0] != "extract" || got[1] != "pdf2md" {
		t.Errorf("tags = %v, want [extract pdf2md]", got)
	}
}

// TestExecute_PropagatesExtractorError: if the extractor fails, no
// writer calls are made and the error reaches the caller.
func TestExecute_PropagatesExtractorError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	boom := errors.New("docling exploded")
	in.Extractor.(*fakeExtractor).err = boom
	_, err := Execute(context.Background(), in)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wraps %v", err, boom)
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 0 {
		t.Error("writer must not be called after extractor failure")
	}
}

// TestExecute_PropagatesWriterError: create failure surfaces to caller.
func TestExecute_PropagatesWriterError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	boom := errors.New("api 500")
	in.Writer.(*fakeNoteWriter).createErr = boom
	_, err := Execute(context.Background(), in)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wraps %v", err, boom)
	}
}

// TestExecute_CachePopulatedOnMiss: first run with a Cache should
// invoke the extractor, then write the markdown into the cache.
func TestExecute_CachePopulatedOnMiss(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "hashA"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.Cache = &MarkdownCache{Dir: filepath.Join(t.TempDir(), "cache")}

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls) != 1 {
		t.Errorf("extractor calls = %d, want 1", atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls))
	}
	if res.Extraction.FromCache {
		t.Error("FromCache=true on a cache miss")
	}
	if _, ok := in.Cache.Get("PDF1", "hashA"); !ok {
		t.Error("cache miss: expected entry populated after Execute")
	}
}

// TestExecute_CacheHitSkipsExtractor: a warm cache short-circuits the
// extractor entirely, but the Zotero post still happens.
func TestExecute_CacheHitSkipsExtractor(t *testing.T) {
	t.Parallel()
	cache := &MarkdownCache{Dir: filepath.Join(t.TempDir(), "cache")}
	if _, err := cache.Put("PDF1", "hashA", []byte("## cached\n")); err != nil {
		t.Fatal(err)
	}

	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "hashA"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.Cache = cache

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls) != 0 {
		t.Errorf("extractor must not be called on cache hit; calls = %d", atomic.LoadInt32(&in.Extractor.(*fakeExtractor).calls))
	}
	if !res.Extraction.FromCache {
		t.Error("FromCache=false on a cache hit")
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 1 {
		t.Fatalf("CreateChildNote calls = %d, want 1", len(w.created))
	}
	if !strings.Contains(w.created[0].body, "cached") {
		t.Errorf("posted body missing cached markdown:\n%s", w.created[0].body)
	}
}

// TestExecute_CachePreservedOnWriterError: if docling succeeds but the
// note post fails, the cache entry must survive so a retry picks up
// the work instead of re-extracting.
func TestExecute_CachePreservedOnWriterError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "hashA"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.Cache = &MarkdownCache{Dir: filepath.Join(t.TempDir(), "cache")}
	in.Writer.(*fakeNoteWriter).createErr = errors.New("api 500")

	if _, err := Execute(context.Background(), in); err == nil {
		t.Fatal("expected writer error")
	}
	if _, ok := in.Cache.Get("PDF1", "hashA"); !ok {
		t.Error("cache entry was dropped after writer failure — resume is broken")
	}
}

// TestExecute_UsesToolVersionFromExtractor: the rendered body must
// reflect the ToolVersion reported by the extractor.
func TestExecute_UsesToolVersionFromExtractor(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.Extractor.(*fakeExtractor).version = "docling 9.9.9"
	_, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	body := in.Writer.(*fakeNoteWriter).created[0].body
	if !strings.Contains(body, "docling 9.9.9") {
		t.Errorf("body missing tool version; got:\n%s", body)
	}
}

// fakeNoteUpdater records every UpdateChildNote call.
type fakeNoteUpdater struct {
	updated   []updateCall
	updateErr error
}

type updateCall struct {
	noteKey string
	body    string
}

func (f *fakeNoteUpdater) UpdateChildNote(_ context.Context, noteKey, body string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = append(f.updated, updateCall{noteKey: noteKey, body: body})
	return nil
}

// TestExecute_Update: when UpdateNoteKey is set, Execute calls
// Updater.UpdateChildNote instead of Writer.CreateChildNote.
func TestExecute_Update(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{
			ParentKey: "PARENT01",
			PDFKey:    "PDF1",
			PDFName:   "paper.pdf",
			PDFHash:   "abc123",
		},
		Action: ActionCreate,
		Reason: "re-extract for update",
	}
	in := baseInput(t, plan)
	updater := &fakeNoteUpdater{}
	in.UpdateNoteKey = "EXISTING"
	in.Updater = updater

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	// Should have called Updater, not Writer.
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 0 {
		t.Errorf("CreateChildNote should NOT be called on update; calls=%d", len(w.created))
	}
	if len(updater.updated) != 1 {
		t.Fatalf("UpdateChildNote calls = %d, want 1", len(updater.updated))
	}
	if updater.updated[0].noteKey != "EXISTING" {
		t.Errorf("noteKey = %q, want EXISTING", updater.updated[0].noteKey)
	}
	if res.NoteKey != "EXISTING" {
		t.Errorf("NoteKey = %q, want EXISTING", res.NoteKey)
	}
	if res.Body == "" {
		t.Error("Body should be non-empty")
	}
}

// TestExecute_UpdateRequiresUpdater: UpdateNoteKey without Updater is an error.
func TestExecute_UpdateRequiresUpdater(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	in.UpdateNoteKey = "EXISTING"
	// in.Updater is nil

	_, err := Execute(context.Background(), in)
	if err == nil {
		t.Fatal("expected error when UpdateNoteKey set but Updater is nil")
	}
}

// TestExecute_UpdatePropagatesError: updater failure surfaces to caller.
func TestExecute_UpdatePropagatesError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{ParentKey: "P", PDFKey: "PDF1", PDFName: "p.pdf", PDFHash: "h"},
		Action:  ActionCreate,
	}
	in := baseInput(t, plan)
	boom := errors.New("api 412")
	in.UpdateNoteKey = "EXISTING"
	in.Updater = &fakeNoteUpdater{updateErr: boom}

	_, err := Execute(context.Background(), in)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wraps %v", err, boom)
	}
}
