package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeExtractor writes a fixed markdown blob to opts.OutputDir and
// returns a synthetic ExtractResult. No docling dependency.
type fakeExtractor struct {
	md      string
	version string
	err     error
	calls   int
}

func (f *fakeExtractor) Extract(_ context.Context, opts ExtractOptions) (*ExtractResult, error) {
	f.calls++
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

// fakeNoteWriter records every CreateChildNote / UpdateChildNote call.
// Errors can be injected per-method.
type fakeNoteWriter struct {
	created   []createCall
	updated   []updateCall
	nextKey   int
	createErr error
	updateErr error
}

type createCall struct {
	parent string
	body   string
	tags   []string
}

type updateCall struct {
	key  string
	body string
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

func (f *fakeNoteWriter) UpdateChildNote(_ context.Context, key, body string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = append(f.updated, updateCall{key: key, body: body})
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

// TestExecute_Create: ActionCreate runs the extractor, renders HTML,
// posts via CreateChildNote with the default docling tag.
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
		Reason: "no existing note",
	}
	in := baseInput(t, plan)

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteKey == "" || !strings.HasPrefix(res.NoteKey, "NOTE") {
		t.Errorf("NoteKey = %q, want NOTE*", res.NoteKey)
	}
	if in.Extractor.(*fakeExtractor).calls != 1 {
		t.Errorf("extractor calls = %d, want 1", in.Extractor.(*fakeExtractor).calls)
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 1 {
		t.Fatalf("CreateChildNote calls = %d, want 1", len(w.created))
	}
	if len(w.updated) != 0 {
		t.Errorf("UpdateChildNote called %d times, want 0", len(w.updated))
	}
	call := w.created[0]
	if call.parent != "PARENT01" {
		t.Errorf("parent = %q", call.parent)
	}
	// Body must be the rendered HTML with the sentinel.
	if !strings.Contains(call.body, "<!-- sci-extract:PDF1:abc123 -->") {
		t.Errorf("body missing sentinel:\n%s", call.body)
	}
	// Default tag applied.
	if len(call.tags) != 1 || call.tags[0] != "docling" {
		t.Errorf("tags = %v, want [docling]", call.tags)
	}
	// Result HTML should match what was posted.
	if res.HTMLBody != call.body {
		t.Error("result HTMLBody differs from posted body")
	}
}

// TestExecute_Replace: ActionReplace runs the extractor and PATCHes
// the existing note key in place — no CreateChildNote call.
func TestExecute_Replace(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request: PlanRequest{
			ParentKey: "PARENT01",
			PDFKey:    "PDF1",
			PDFName:   "paper.pdf",
			PDFHash:   "NEWHASH",
		},
		Action:       ActionReplace,
		Reason:       "pdf hash changed",
		ExistingNote: "OLDNOTE1",
	}
	in := baseInput(t, plan)

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteKey != "OLDNOTE1" {
		t.Errorf("NoteKey = %q, want OLDNOTE1", res.NoteKey)
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created) != 0 {
		t.Errorf("CreateChildNote calls = %d, want 0", len(w.created))
	}
	if len(w.updated) != 1 {
		t.Fatalf("UpdateChildNote calls = %d, want 1", len(w.updated))
	}
	if w.updated[0].key != "OLDNOTE1" {
		t.Errorf("updated key = %q", w.updated[0].key)
	}
	if !strings.Contains(w.updated[0].body, "<!-- sci-extract:PDF1:NEWHASH -->") {
		t.Error("update body missing new-hash sentinel")
	}
}

// TestExecute_Skip: ActionSkip must short-circuit — no extractor run,
// no writer calls, result carries the existing note key.
func TestExecute_Skip(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Request:      PlanRequest{ParentKey: "PARENT01", PDFKey: "PDF1", PDFHash: "abc123"},
		Action:       ActionSkip,
		Reason:       "up-to-date",
		ExistingNote: "UPTODATE1",
	}
	in := baseInput(t, plan)

	res, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteKey != "UPTODATE1" {
		t.Errorf("NoteKey = %q, want UPTODATE1", res.NoteKey)
	}
	if in.Extractor.(*fakeExtractor).calls != 0 {
		t.Errorf("Skip must not invoke extractor; calls = %d", in.Extractor.(*fakeExtractor).calls)
	}
	w := in.Writer.(*fakeNoteWriter)
	if len(w.created)+len(w.updated) != 0 {
		t.Errorf("Skip must not call writer; created=%d updated=%d", len(w.created), len(w.updated))
	}
	if res.HTMLBody != "" {
		t.Errorf("Skip should leave HTMLBody empty; got %q", res.HTMLBody)
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
	if len(w.created)+len(w.updated) != 0 {
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

// TestExecute_UsesToolVersionFromExtractor: the rendered HTML must
// reflect the ToolVersion reported by the extractor (not a hardcode).
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
