package cli

// Unit tests for the pure helpers behind `zot item note add`. The live CLI
// Action is not mocked (matches the existing collection_add_test.go
// convention); it's covered by a post-build sanity run against the lab's
// `eshin` collection.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

// --- readNoteBody ---

func TestReadNoteBody_fromFlag(t *testing.T) {
	t.Parallel()
	got, err := readNoteBody("hello world", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestReadNoteBody_fromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	want := "# Title\n\nBody with **bold**.\n"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readNoteBody("", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadNoteBody_fromStdinViaDash(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("streamed body\n")
	got, err := readNoteBody("-", "", stdin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "streamed body\n" {
		t.Errorf("got %q, want %q", got, "streamed body\n")
	}
}

func TestReadNoteBody_fromStdinViaFileDash(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("file-dash body")
	got, err := readNoteBody("", "-", stdin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "file-dash body" {
		t.Errorf("got %q, want %q", got, "file-dash body")
	}
}

func TestReadNoteBody_mutuallyExclusive(t *testing.T) {
	t.Parallel()
	_, err := readNoteBody("inline", "/tmp/some.md", nil)
	if err == nil {
		t.Fatal("expected error when --body and --body-file both set")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "body") {
		t.Errorf("err=%v should mention --body/--body-file", err)
	}
}

func TestReadNoteBody_missing(t *testing.T) {
	t.Parallel()
	_, err := readNoteBody("", "", nil)
	if err == nil {
		t.Fatal("expected error when no body source is given")
	}
}

func TestReadNoteBody_fileNotFound(t *testing.T) {
	t.Parallel()
	_, err := readNoteBody("", "/definitely/not/a/path/note.md", nil)
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

// --- renderNoteBody ---

func TestRenderNoteBody_markdownByDefault(t *testing.T) {
	t.Parallel()
	got, err := renderNoteBody("**bold**", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("markdown not rendered: %q", got)
	}
}

func TestRenderNoteBody_htmlPassthroughSanitizes(t *testing.T) {
	t.Parallel()
	// Even with --html, hostile HTML must be stripped.
	got, err := renderNoteBody(`<p>safe</p><script>alert(1)</script>`, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<p>safe</p>") {
		t.Errorf("safe HTML dropped: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "<script") {
		t.Errorf("<script> survived: %q", got)
	}
}

func TestRenderNoteBody_htmlSkipsMarkdownParse(t *testing.T) {
	t.Parallel()
	// In --html mode, markdown syntax is NOT parsed — literal asterisks stay.
	got, err := renderNoteBody("not **markdown**", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "<strong>") {
		t.Errorf("--html mode wrongly parsed markdown: %q", got)
	}
	if !strings.Contains(got, "not **markdown**") {
		t.Errorf("literal asterisks lost: %q", got)
	}
}

// --- buildNoteItemData ---

func TestBuildNoteItemData_standalone(t *testing.T) {
	t.Parallel()
	got := buildNoteItemData("<p>hi</p>", "", "COLL1234", nil)
	if got.ItemType != client.Note {
		t.Errorf("ItemType = %q, want note", got.ItemType)
	}
	if got.Note == nil || *got.Note != "<p>hi</p>" {
		t.Errorf("Note = %v", got.Note)
	}
	if got.ParentItem != nil {
		t.Errorf("ParentItem must be nil for standalone, got %v", *got.ParentItem)
	}
	if got.Collections == nil || len(*got.Collections) != 1 || (*got.Collections)[0] != "COLL1234" {
		t.Errorf("Collections = %v, want [COLL1234]", got.Collections)
	}
}

func TestBuildNoteItemData_attachedToParent(t *testing.T) {
	t.Parallel()
	got := buildNoteItemData("<p>hi</p>", "PARENT12", "", nil)
	if got.ParentItem == nil || *got.ParentItem != "PARENT12" {
		t.Errorf("ParentItem = %v, want PARENT12", got.ParentItem)
	}
	if got.Collections != nil {
		t.Errorf("Collections must be nil when only parent set, got %v", *got.Collections)
	}
}

func TestBuildNoteItemData_parentAndCollection(t *testing.T) {
	t.Parallel()
	// Both are legal in Zotero — the note is a child of the parent AND
	// surfaces in the named collection.
	got := buildNoteItemData("<p>hi</p>", "PARENT12", "COLL1234", nil)
	if got.ParentItem == nil || *got.ParentItem != "PARENT12" {
		t.Errorf("ParentItem missing: %v", got.ParentItem)
	}
	if got.Collections == nil || (*got.Collections)[0] != "COLL1234" {
		t.Errorf("Collections missing: %v", got.Collections)
	}
}

func TestBuildNoteItemData_withTags(t *testing.T) {
	t.Parallel()
	got := buildNoteItemData("<p>hi</p>", "", "COLL1234", []string{"lit-review", "sr"})
	if got.Tags == nil || len(*got.Tags) != 2 {
		t.Fatalf("Tags = %v", got.Tags)
	}
	names := []string{(*got.Tags)[0].Tag, (*got.Tags)[1].Tag}
	if names[0] != "lit-review" || names[1] != "sr" {
		t.Errorf("tag names = %v", names)
	}
}

func TestBuildNoteItemData_noTags(t *testing.T) {
	t.Parallel()
	got := buildNoteItemData("<p>hi</p>", "", "COLL1234", nil)
	if got.Tags != nil {
		t.Errorf("Tags should be nil when none given, got %v", *got.Tags)
	}
}

// --- validateNoteTarget ---

func TestValidateNoteTarget_bothMissing(t *testing.T) {
	t.Parallel()
	err := validateNoteTarget("", "")
	if err == nil {
		t.Fatal("expected error when neither parent nor collection is set")
	}
}

func TestValidateNoteTarget_parentOnly(t *testing.T) {
	t.Parallel()
	if err := validateNoteTarget("PARENT12", ""); err != nil {
		t.Errorf("parent-only should be valid: %v", err)
	}
}

func TestValidateNoteTarget_collectionOnly(t *testing.T) {
	t.Parallel()
	if err := validateNoteTarget("", "COLL1234"); err != nil {
		t.Errorf("collection-only should be valid: %v", err)
	}
}

func TestValidateNoteTarget_both(t *testing.T) {
	t.Parallel()
	if err := validateNoteTarget("PARENT12", "COLL1234"); err != nil {
		t.Errorf("parent+collection should be valid: %v", err)
	}
}
