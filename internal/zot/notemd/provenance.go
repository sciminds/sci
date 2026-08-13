package notemd

import "strings"

// maxProvenanceLines bounds how far into a document the provenance block
// may extend. sci writes seven fields; the cap is what makes "find the
// closing fence" safe on a paper that opens with a horizontal rule,
// since the search gives up long before it reaches the next one.
const maxProvenanceLines = 12

// provenanceMarker is the field sci writes first in every extraction
// note, and the positive evidence that a leading YAML block is ours.
const provenanceMarker = "zotero_key:"

// StripProvenance removes the YAML provenance block that an extraction
// note carries, returning the paper's text alone.
//
// The block is metadata about the extraction — the parent item key, the
// PDF key, the title, the docling source, a hash and a date — so anything
// that shows the *beginning* of a note has to drop it first. A preview
// that keeps it reads `--- zotero_key: … title: "…" source…` instead of
// the paper's first sentence, and the paper's own title is the one line
// of it a reader can already see.
//
// Stripping is gated on positive evidence, the same discipline as
// [IsHTMLNote]: the text must open with a `---` fence, close it
// within [maxProvenanceLines], and carry [provenanceMarker] inside.
// Anything else — a paper that opens with a horizontal rule, a document
// with its own frontmatter, Zotero's plain-text cache — is returned
// unchanged. Guessing wrong here silently eats the top of a paper.
//
// The HTML note format writes its provenance as a heading plus an italic
// line instead, which this does not detect. That is deliberate: it has no
// unambiguous marker, and in the live library essentially every
// extraction is markdown.
func StripProvenance(text string) string {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return text
	}
	lines := strings.SplitAfter(rest, "\n")

	marked, consumed := false, 0
	for i, line := range lines {
		if i >= maxProvenanceLines {
			return text
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, provenanceMarker) {
			marked = true
		}
		if trimmed == "---" {
			consumed = i + 1
			break
		}
	}
	if !marked || consumed == 0 {
		return text
	}
	body := strings.Join(lines[consumed:], "")
	return strings.TrimLeft(body, "\n")
}
