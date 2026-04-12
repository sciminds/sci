package extract

import (
	"strings"
	"testing"
	"time"
)

// TestMarkdownToNoteHTML_RendersWithHeader locks three contracts:
//   - The header block contains filename + source + date + short hash.
//   - Docling's `<!-- image -->` placeholders are replaced with a visible
//     marker before goldmark runs, so Zotero's note pane renders them.
//   - No sentinel comments are emitted.
func TestMarkdownToNoteHTML_RendersWithHeader(t *testing.T) {
	t.Parallel()
	md := []byte("## Abstract\n\nChronic kidney disease is common.\n\n<!-- image -->\n\n- one\n- two\n")
	meta := NoteMeta{
		ParentKey: "PARENT1",
		PDFKey:    "7T798XVD",
		PDFName:   "Webster et al. - 2017 - Chronic kidney disease.pdf",
		Source:    "docling 2.86.0",
		Hash:      "abc123def456",
		Generated: time.Date(2026, 4, 11, 18, 30, 0, 0, time.UTC),
	}

	got := MarkdownToNoteHTML(md, meta)

	want := []string{
		// Header block — human-readable metadata
		"<h1>Webster et al. - 2017 - Chronic kidney disease.pdf</h1>",
		"docling 2.86.0",
		"2026-04-11",
		"fp:abc123def456",
		// Rendered body
		"Abstract",
		"<p>Chronic kidney disease is common.</p>",
		"<li>one</li>",
		// Figure placeholder rendered as visible italic text
		"<em>(figure)</em>",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("rendered HTML missing %q\n---\n%s", s, got)
		}
	}

	// Raw HTML comment must not survive.
	if strings.Contains(got, "<!-- image -->") {
		t.Errorf("raw <!-- image --> placeholder leaked into output:\n%s", got)
	}
	// No sentinel comments.
	if strings.Contains(got, "sci-extract") {
		t.Errorf("sentinel comment found in output:\n%s", got)
	}
}

// TestMarkdownToNoteHTML_EscapesPDFName guards against a malicious or
// oddly-named attachment title breaking out of the <h1>.
func TestMarkdownToNoteHTML_EscapesPDFName(t *testing.T) {
	t.Parallel()
	meta := NoteMeta{
		ParentKey: "P1",
		PDFKey:    "KEY1",
		PDFName:   "<script>alert(1)</script>.pdf",
		Source:    "docling 2.86.0",
		Hash:      "deadbeef",
		Generated: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
	}
	got := MarkdownToNoteHTML([]byte("body"), meta)
	if strings.Contains(got, "<script>alert") {
		t.Errorf("unescaped script tag in PDFName:\n%s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag in output:\n%s", got)
	}
}

// TestMarkdownToNoteRaw_YAMLFrontmatter: the raw-md variant must produce
// YAML frontmatter with all required fields and preserve the original
// markdown body.
func TestMarkdownToNoteRaw_YAMLFrontmatter(t *testing.T) {
	t.Parallel()
	md := []byte("## Abstract\n\nCKD is <common> & widespread.\n")
	meta := NoteMeta{
		ParentKey: "PARENT1",
		PDFKey:    "7T798XVD",
		PDFName:   "CKD paper.pdf",
		DOI:       "10.1016/S0140-6736(16)32064-5",
		Source:    "docling 2.86.0",
		Hash:      "abc123def456",
		Generated: time.Date(2026, 4, 11, 18, 30, 0, 0, time.UTC),
	}
	got := MarkdownToNoteRaw(md, meta)

	want := []string{
		"---",
		"zotero_key: PARENT1",
		"pdf_key: 7T798XVD",
		`title: "CKD paper.pdf"`,
		`doi: "10.1016/S0140-6736(16)32064-5"`,
		"source: docling 2.86.0",
		"hash: abc123def456",
		"generated: 2026-04-11",
		"## Abstract",  // markdown preserved, not rendered
		"<common>",     // raw markdown, not escaped
		"& widespread", // raw ampersand, not escaped
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("raw note missing %q\n---\n%s", s, got)
		}
	}

	// No sentinel comments.
	if strings.Contains(got, "sci-extract") {
		t.Errorf("sentinel comment found in output:\n%s", got)
	}
	// No HTML rendering.
	if strings.Contains(got, "<h2") {
		t.Errorf("unexpected HTML rendering in raw output:\n%s", got)
	}
}

// TestMarkdownToNoteRaw_OmitsDOIWhenEmpty: frontmatter must not include
// a doi field when DOI is empty.
func TestMarkdownToNoteRaw_OmitsDOIWhenEmpty(t *testing.T) {
	t.Parallel()
	meta := NoteMeta{
		ParentKey: "P1",
		PDFKey:    "KEY1",
		PDFName:   "test.pdf",
		Source:    "docling 2.86.0",
		Hash:      "h",
		Generated: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
	}
	got := MarkdownToNoteRaw([]byte("body"), meta)
	if strings.Contains(got, "doi:") {
		t.Errorf("doi field should not appear when empty:\n%s", got)
	}
}
