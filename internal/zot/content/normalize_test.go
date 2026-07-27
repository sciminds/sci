package content

import (
	"strings"
	"testing"
)

// The provenance block sci writes on every extraction note (see
// extract.MarkdownToNoteRaw).
const sampleProvenance = `---
zotero_key: 5ABS8B8G
pdf_key: UIQV9AXD
title: "Prediction Errors Disrupt Hippocampal Representations"
doi: "10.1101/2020.09.29.319418"
source: docling (cached)
hash: 2074691-1775813983
generated: 2026-04-22
---

## Prediction Errors Disrupt Hippocampal Representations

Alyssa H. Sinclair, Grace M. Manalili`

func TestStripProvenanceRemovesTheBlock(t *testing.T) {
	got := stripProvenance(sampleProvenance)

	for _, gone := range []string{"zotero_key", "pdf_key", "docling (cached)", "2074691", "generated"} {
		if strings.Contains(got, gone) {
			t.Errorf("provenance field %q survived:\n%s", gone, got)
		}
	}
	if !strings.HasPrefix(got, "## Prediction Errors") {
		t.Errorf("body does not start at the paper's own text:\n%q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, "Alyssa H. Sinclair") {
		t.Error("body text was lost")
	}
}

// A paper whose text happens to open with a horizontal rule is not our
// note format, and eating up to its next `---` would swallow real content.
func TestStripProvenanceLeavesForeignBlocksAlone(t *testing.T) {
	cases := map[string]string{
		"horizontal rule":            "---\n\nThe paper opens with a rule.\n\n---\n\nMore text.",
		"someone else's frontmatter": "---\ntitle: A Paper\nauthor: Someone\n---\n\nBody.",
		"unterminated fence":         "---\nzotero_key: AAAA1111\ntitle: no closing fence\n\nBody text.",
		"no frontmatter":             "## Just a heading\n\nBody text.",
		"empty":                      "",
	}
	for name, text := range cases {
		if got := stripProvenance(text); got != text {
			t.Errorf("%s: text was modified\n got: %q\nwant: %q", name, got, text)
		}
	}
}

// The block is only ours when it carries the key sci writes first; a
// long YAML header that never mentions it is someone else's document.
func TestStripProvenanceRequiresTheMarkerNearTheTop(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\n")
	for range maxProvenanceLines {
		b.WriteString("filler: value\n")
	}
	b.WriteString("zotero_key: AAAA1111\n---\n\nBody.")

	text := b.String()
	if got := stripProvenance(text); got != text {
		t.Error("stripped a block whose marker was past the provenance-block size")
	}
}

// The Zotero text cache is plain pdftotext output — no frontmatter, and
// nothing that looks like it should be touched.
func TestStripProvenanceIsANoOpForZoteroCacheText(t *testing.T) {
	text := "Prediction Errors Disrupt Hippocampal Representations\nAlyssa H. Sinclair"
	if got := stripProvenance(text); got != text {
		t.Errorf("plain text was modified: %q", got)
	}
}
