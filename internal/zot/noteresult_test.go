package zot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestNotesListResult_JSON(t *testing.T) {
	t.Parallel()
	r := NotesListResult{Count: 1, Notes: []local.DoclingNoteSummary{{NoteKey: "N1"}}}
	if r.JSON() == nil {
		t.Error("JSON() returned nil")
	}
}

func TestNotesListResult_HumanEmpty(t *testing.T) {
	t.Parallel()
	r := NotesListResult{Count: 0}
	h := r.Human()
	// "extractions", not "notes": this result now backs `zot content list`,
	// and its empty state must not read like `zot notes list`'s.
	if !strings.Contains(h, "no extractions") {
		t.Errorf("expected empty message; got: %s", h)
	}
}

func TestNotesListResult_HumanWithNotes(t *testing.T) {
	t.Parallel()
	r := NotesListResult{
		Count: 1,
		Notes: []local.DoclingNoteSummary{
			{NoteKey: "NOTE1", ParentKey: "PAR1", ParentTitle: "My Paper", Body: "<p>hello</p>"},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "NOTE1") || !strings.Contains(h, "PAR1") {
		t.Errorf("missing keys in output: %s", h)
	}
	if !strings.Contains(h, "1 note(s)") {
		t.Errorf("missing count: %s", h)
	}
}

// TestNotesListResult_HumanPagination — when Total > Offset+Count the
// renderer surfaces a "showing M-N of T" footer with the next --offset
// hint, so an agent paginating sees how to advance without consulting
// docs. Single-page lists keep the older "→ N note(s)" footer.
func TestNotesListResult_HumanPagination(t *testing.T) {
	t.Parallel()
	r := NotesListResult{
		Count:  2,
		Total:  10,
		Offset: 4,
		Notes: []local.DoclingNoteSummary{
			{NoteKey: "N1", ParentKey: "P1"},
			{NoteKey: "N2", ParentKey: "P2"},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "showing 5-6 of 10") {
		t.Errorf("missing pagination header: %s", h)
	}
	if !strings.Contains(h, "--offset 6") {
		t.Errorf("missing next-offset hint: %s", h)
	}
	if !strings.Contains(h, "--limit 0") {
		t.Errorf("missing unlimited hint: %s", h)
	}
}

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

func TestNoteAddResult_Human(t *testing.T) {
	t.Parallel()
	r := NoteAddResult{
		ParentKey: "P1", PDFName: "paper.pdf", NoteKey: "N1",
		Action: "create", ToolVersion: "docling 2.86.0", Duration: 5 * time.Second,
	}
	h := r.Human()
	if !strings.Contains(h, "created note N1") {
		t.Errorf("missing create message: %s", h)
	}
}

func TestNoteAddResult_HumanSkip(t *testing.T) {
	t.Parallel()
	r := NoteAddResult{Action: "skip", PDFName: "paper.pdf"}
	h := r.Human()
	if !strings.Contains(h, "skipped") {
		t.Errorf("expected skip message: %s", h)
	}
}

func TestNoteUpdateResult_Human(t *testing.T) {
	t.Parallel()
	r := NoteUpdateResult{
		ParentKey: "P1", PDFName: "paper.pdf", NoteKey: "N1",
		ToolVersion: "docling 2.86.0", Duration: 3 * time.Second,
	}
	h := r.Human()
	if !strings.Contains(h, "updated note N1") {
		t.Errorf("missing update message: %s", h)
	}
}

func TestNoteDeleteResult_HumanEmpty(t *testing.T) {
	t.Parallel()
	r := NoteDeleteResult{ParentKey: "P1"}
	h := r.Human()
	if !strings.Contains(h, "no docling notes found") {
		t.Errorf("expected empty message: %s", h)
	}
}

func TestNoteDeleteResult_HumanWithResults(t *testing.T) {
	t.Parallel()
	r := NoteDeleteResult{
		ParentKey: "P1", Total: 2,
		Trashed: []string{"N1"},
		Failed:  map[string]string{"N2": "api 500"},
	}
	h := r.Human()
	if !strings.Contains(h, "trashed note N1") {
		t.Errorf("missing trashed message: %s", h)
	}
	if !strings.Contains(h, "N2: api 500") {
		t.Errorf("missing failed message: %s", h)
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
