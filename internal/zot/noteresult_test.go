package zot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/sci/pkg/local"
)

func TestNoteReadResult_Human(t *testing.T) {
	t.Parallel()
	r := NoteReadResult{Note: local.NoteDetail{
		Key: "N1", ParentKey: "P1", Title: "My Note",
		Body: "<p>Hello world</p>", Tags: []string{"docling"},
	}}
	h := r.Human()
	if !strings.Contains(h, "N1") || !strings.Contains(h, "P1") {
		t.Errorf("missing key info: %s", h)
	}
	if !strings.Contains(h, "Hello world") {
		t.Errorf("body not stripped of HTML: %s", h)
	}
	if !strings.Contains(h, "docling") {
		t.Errorf("missing tag: %s", h)
	}
}

// TestNoteBodyForDisplay replaces the old stripHTML test. The display path
// now keeps markdown structure instead of flattening to bare text, so
// emphasis and headings survive to the terminal.
func TestNoteBodyForDisplay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<b>bold</b> and <i>italic</i>", "**bold** and *italic*"},
		{"<h2>Section</h2>", "## Section"},
		{"no tags", "no tags"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := noteBodyForDisplay(tc.in); got != tc.want {
			t.Errorf("noteBodyForDisplay(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNoteReadResult_HumanRendersMarkdown — the old stripHTML display threw
// away every heading, list marker and link, which for a 20-page extraction
// note is most of the information. Markdown keeps the structure.
func TestNoteReadResult_HumanRendersMarkdown(t *testing.T) {
	t.Parallel()
	r := NoteReadResult{Note: local.NoteDetail{
		Key:  "NOTECH10",
		Body: `<div class="zotero-note znv1"><h2>Central Thesis</h2><ul><li>Wisdom is a process.</li></ul></div>`,
	}}
	h := r.Human()
	if !strings.Contains(h, "## Central Thesis") {
		t.Errorf("heading flattened:\n%s", h)
	}
	if !strings.Contains(h, "- Wisdom is a process.") {
		t.Errorf("list marker lost:\n%s", h)
	}
	if strings.Contains(h, "<h2>") || strings.Contains(h, "znv1") {
		t.Errorf("markup leaked:\n%s", h)
	}
}

// TestNoteReadResult_JSONCarriesMarkdownOnlyWhenAsked keeps the default
// --json shape byte-identical for existing agents; --md is opt-in.
func TestNoteReadResult_JSONCarriesMarkdownOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	note := local.NoteDetail{Key: "NOTECH10", Body: "<p>Body.</p>"}

	plain, err := json.Marshal(NoteReadResult{Note: note}.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "markdown") {
		t.Errorf("default shape grew a markdown field: %s", plain)
	}

	withMD, err := json.Marshal(NoteReadResult{Note: note, Markdown: "Body."}.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(withMD, &got); err != nil {
		t.Fatal(err)
	}
	if got["markdown"] != "Body." {
		t.Errorf("markdown = %v, want %q", got["markdown"], "Body.")
	}
	// The original fields must survive alongside it.
	if got["key"] != "NOTECH10" {
		t.Errorf("note fields dropped: %s", withMD)
	}
}
