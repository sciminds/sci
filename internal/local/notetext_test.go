package local

import (
	"strings"
	"testing"
)

// Every Zotero note is wrapped in `<div class="zotero-note znv1">`.
// If that markup survived into the indexed text, those words would match
// every paper in the library.
func TestNoteTextStripsMarkup(t *testing.T) {
	const body = `<div class="zotero-note znv1"><h1>Successor Representations</h1>` +
		`<p>The map factorizes graph communicability.</p></div>`

	got := noteText(body)
	for _, markup := range []string{"div", "class", "znv1", "zotero-note", "<", ">"} {
		if strings.Contains(got, markup) {
			t.Errorf("noteText kept markup %q: %q", markup, got)
		}
	}
	if !strings.Contains(got, "Successor Representations") {
		t.Errorf("noteText dropped the content: %q", got)
	}
}

// Tags become a space, not nothing — otherwise a heading and the
// paragraph after it weld into a token nobody wrote.
func TestNoteTextDoesNotWeldAdjacentTags(t *testing.T) {
	got := noteText(`<h1>Alpha</h1><p>Beta</p>`)
	if got != "Alpha Beta" {
		t.Errorf("noteText = %q, want %q", got, "Alpha Beta")
	}
}

// Zotero stores "&" as "&amp;". Leaving it encoded would make the query
// "communicability & predictive" unmatchable.
func TestNoteTextDecodesEntities(t *testing.T) {
	got := noteText(`<p>communicability &amp; predictive maps</p>`)
	if !strings.Contains(got, "communicability & predictive") {
		t.Errorf("noteText left the entity encoded: %q", got)
	}
}

func TestNoteTextCollapsesWhitespace(t *testing.T) {
	got := noteText("<p>one</p>\n\n   <p>two</p>\t")
	if got != "one two" {
		t.Errorf("noteText = %q, want %q", got, "one two")
	}
}

func TestNoteTextEmpty(t *testing.T) {
	if got := noteText(""); got != "" {
		t.Errorf("noteText(\"\") = %q, want empty", got)
	}
}
