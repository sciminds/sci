package zot

import (
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
	if !strings.Contains(h, "no docling notes") {
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

func TestStripHTML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<b>bold</b> and <i>italic</i>", "bold and italic"},
		{"no tags", "no tags"},
		{"", ""},
	}
	for _, tc := range cases {
		got := stripHTML(tc.in)
		if got != tc.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
