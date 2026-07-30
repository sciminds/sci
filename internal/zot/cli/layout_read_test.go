package cli

// Tests for the layout-store fallback on the note-reading verbs.
//
// An extraction lands in two INDEPENDENT stores: the Zotero child note
// and the per-key layout dir. Items whose markdown exceeds Zotero's
// ~500KB note limit have only the second one (28 of them live, as of
// Jul 2026), and before this fallback `content read` / `llm read`
// answered "no content" for papers sci had fully extracted.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// withLayoutConfig extends the orient fixture with a configured
// extract.dir and isolates os.UserCacheDir (via HOME) so the content
// index a test opens is always an empty one in a temp dir.
func withLayoutConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	withOrientConfig(t)

	extractDir := t.TempDir()
	cfg, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Extract.Dir = extractDir
	if err := zot.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return extractDir
}

// seedLayoutDir writes a completed key dir: both payload files plus the
// .done marker, matching what extract.KeyLayout.Finalize produces.
func seedLayoutDir(t *testing.T, extractDir, key, markdown string) {
	t.Helper()
	keyDir := filepath.Join(extractDir, key)
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", keyDir, err)
	}
	files := map[string]string{
		key + ".md":   markdown,
		key + ".json": `{"tables":[],"pictures":[],"pages":{"1":{}}}`,
		".done":       "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(keyDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

var layoutJSONOutput bool

// runLayoutZot drives the content/llm command trees over the fixture.
func runLayoutZot(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	done := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- buf
	}()

	// Only the two leaves under test, for the same reason runZot trims
	// its tree: the full Commands() tree rebinds package-level slice-flag
	// destinations other tests mutate.
	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(&layoutJSONOutput),
		}, PersistentFlags()...),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{Name: "content", Commands: []*cli.Command{contentReadCommand()}},
			{Name: "llm", Commands: []*cli.Command{llmReadCommand()}},
		},
	}
	runErr := root.Run(context.Background(), slices.Concat([]string{"zot"}, args))

	_ = w.Close()
	return <-done, runErr
}

func TestContentRead_FallsBackToLayoutMarkdown(t *testing.T) {
	extractDir := withLayoutConfig(t)
	// KEY2 has no docling note in the fixture — layout store only.
	seedLayoutDir(t, extractDir, "KEY2", "# Paper Two\n\nBody text from the layout store.\n")

	out, err := runLayoutZot(t, "--json", "--library", "personal", "content", "read", "KEY2")
	if err != nil {
		t.Fatalf("content read KEY2: %v\n%s", err, string(out))
	}
	var got struct {
		ItemKey string `json:"item_key"`
		Source  string `json:"source"`
		Chars   int    `json:"chars"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Source != "layout" {
		t.Errorf("source = %q, want %q", got.Source, "layout")
	}
	if !strings.Contains(got.Body, "Body text from the layout store") {
		t.Errorf("body did not come from the layout markdown: %q", got.Body)
	}
	if got.Chars != len(got.Body) {
		t.Errorf("chars = %d, want %d", got.Chars, len(got.Body))
	}
}

func TestContentRead_NoNoteNoLayout_StillNotFound(t *testing.T) {
	withLayoutConfig(t)

	_, err := runLayoutZot(t, "--library", "personal", "content", "read", "KEY3")
	if err == nil {
		t.Fatal("expected a not-found error for a key with neither store")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want a coded error, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeNotFound {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeNotFound)
	}
}

func TestContentRead_IncompleteLayoutDir_NotServed(t *testing.T) {
	extractDir := withLayoutConfig(t)
	// Markdown present but no .done marker: an interrupted or failed
	// extraction must never be served as if it were the whole paper.
	keyDir := filepath.Join(extractDir, "KEY3")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "KEY3.md"), []byte("# truncated"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runLayoutZot(t, "--library", "personal", "content", "read", "KEY3")
	if err == nil {
		t.Fatal("expected an error — a dir without .done is not a finished extraction")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || coded.Code != cmdutil.CodeNotFound {
		t.Fatalf("want a not-found error, got %T: %v", err, err)
	}
}

// The layout store is one flat KEY→dir map with no library dimension —
// both libraries extract into the same extract.dir. Serving from it
// without a scope check made `content read --library personal` answer
// with a shared-library paper (caught live on Marr's Vision, 68RQ6ZGN).
func TestContentRead_LayoutFallback_RespectsLibraryScope(t *testing.T) {
	extractDir := withLayoutConfig(t)
	// GRPKEY01 lives in the group library (libraryID=2) in the fixture.
	seedLayoutDir(t, extractDir, "GRPKEY01", "# Shared Cortex Paper\n\nGroup-library body.\n")

	out, err := runLayoutZot(t, "--json", "--library", "personal", "content", "read", "GRPKEY01")
	if err == nil {
		t.Fatalf("personal scope served a group-library paper from the layout store:\n%s", string(out))
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || coded.Code != cmdutil.CodeNotFound {
		t.Fatalf("want a not-found error, got %T: %v", err, err)
	}
}

func TestLLMRead_FallsBackToLayoutMarkdown(t *testing.T) {
	extractDir := withLayoutConfig(t)
	seedLayoutDir(t, extractDir, "KEY2", "# Paper Two\n\nLayout body.\n")

	out, err := runLayoutZot(t, "--json", "--library", "personal", "llm", "read", "KEY2")
	if err != nil {
		t.Fatalf("llm read KEY2: %v\n%s", err, string(out))
	}
	var got struct {
		Count   int `json:"count"`
		Entries []struct {
			Key     string `json:"key"`
			Title   string `json:"title"`
			NoteKey string `json:"note_key"`
			Source  string `json:"source"`
			Body    string `json:"body"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Count != 1 {
		t.Fatalf("count = %d, want 1", got.Count)
	}
	e := got.Entries[0]
	if e.Source != "layout" {
		t.Errorf("source = %q, want %q", e.Source, "layout")
	}
	if e.NoteKey != "" {
		t.Errorf("note_key = %q, want empty — there is no note", e.NoteKey)
	}
	if e.Title != "Paper Two" {
		t.Errorf("title = %q, want %q — parent metadata still comes from the DB", e.Title, "Paper Two")
	}
	if !strings.Contains(e.Body, "Layout body") {
		t.Errorf("body = %q, want the layout markdown", e.Body)
	}
}

// The note is the primary store: when both exist, nothing changes.
func TestLLMRead_NoteWinsOverLayout(t *testing.T) {
	extractDir := withLayoutConfig(t)
	// KEY1 owns DOCL0001 in the fixture.
	seedLayoutDir(t, extractDir, "KEY1", "# from the layout store\n")

	out, err := runLayoutZot(t, "--json", "--library", "personal", "llm", "read", "KEY1")
	if err != nil {
		t.Fatalf("llm read KEY1: %v\n%s", err, string(out))
	}
	var got struct {
		Entries []struct {
			NoteKey string `json:"note_key"`
			Source  string `json:"source"`
			Body    string `json:"body"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.NoteKey != "DOCL0001" {
		t.Errorf("note_key = %q, want DOCL0001 — the note must win", e.NoteKey)
	}
	if e.Source != "" {
		t.Errorf("source = %q, want empty — note-sourced entries keep the original JSON shape", e.Source)
	}
	if strings.Contains(e.Body, "from the layout store") {
		t.Error("body came from the layout store, but a note exists")
	}
}

func TestLLMRead_NoNoteNoLayout_StillErrors(t *testing.T) {
	withLayoutConfig(t)

	_, err := runLayoutZot(t, "--library", "personal", "llm", "read", "KEY3")
	if err == nil {
		t.Fatal("expected an error for a key with neither store")
	}
	if !strings.Contains(err.Error(), "no docling note found for KEY3") {
		t.Errorf("error = %v, want the original no-note message preserved", err)
	}
}

// TestLayoutExtraction_UnconfiguredDir pins that the fallback is inert
// when extract.dir is unset — the whole feature must be invisible to
// users who never configured a layout store.
func TestLayoutExtraction_UnconfiguredDir(t *testing.T) {
	t.Parallel()
	// nil db is safe: an unconfigured store returns before any query.
	body, ok, err := layoutExtraction(&zot.Config{}, nil, "KEY2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || body != "" {
		t.Errorf("got (%q, %v), want ('', false) when extract.dir is unset", body, ok)
	}
}
