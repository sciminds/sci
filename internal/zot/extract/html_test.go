package extract

import (
	"strings"
	"testing"
	"time"
)

// TestMarkdownToNoteHTML_RendersWithSentinel locks three contracts:
//   - The header block contains filename + source + date + short hash.
//   - The sentinel comment shape `<!-- sci-extract:KEY:HASH -->` must not
//     drift, because FindSentinel parses it.
//   - Docling's `<!-- image -->` placeholders are replaced with a visible
//     marker before goldmark runs, so Zotero's note pane renders them.
func TestMarkdownToNoteHTML_RendersWithSentinel(t *testing.T) {
	t.Parallel()
	md := []byte("## Abstract\n\nChronic kidney disease is common.\n\n<!-- image -->\n\n- one\n- two\n")
	meta := NoteMeta{
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
		"sha256:abc123def456",
		// Sentinel — exact shape must not drift; PlanExtract parses it
		"<!-- sci-extract:7T798XVD:abc123def456 -->",
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

	// Raw HTML comment must not survive — Zotero's note renderer strips
	// comments, so if we left it in the figure would be invisible.
	if strings.Contains(got, "<!-- image -->") {
		t.Errorf("raw <!-- image --> placeholder leaked into output:\n%s", got)
	}
}

// TestMarkdownToNoteHTML_EscapesPDFName guards against a malicious or
// oddly-named attachment title breaking out of the <h1>.
func TestMarkdownToNoteHTML_EscapesPDFName(t *testing.T) {
	t.Parallel()
	meta := NoteMeta{
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

func TestFindSentinel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		body     string
		wantKey  string
		wantHash string
		wantOk   bool
	}{
		{
			name:     "match in middle of body",
			body:     "<p>header</p><!-- sci-extract:KEY1:abc123 --><hr><p>body</p>",
			wantKey:  "KEY1",
			wantHash: "abc123",
			wantOk:   true,
		},
		{
			name:   "no sentinel",
			body:   "<p>plain note</p>",
			wantOk: false,
		},
		{
			name:   "unrelated comment",
			body:   "<!-- note --><p>x</p>",
			wantOk: false,
		},
		{
			name:   "malformed payload (missing colon)",
			body:   "<!-- sci-extract:KEYONLY -->",
			wantOk: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, h, ok := FindSentinel(tc.body)
			if ok != tc.wantOk || k != tc.wantKey || h != tc.wantHash {
				t.Errorf("FindSentinel(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.body, k, h, ok, tc.wantKey, tc.wantHash, tc.wantOk)
			}
		})
	}
}
