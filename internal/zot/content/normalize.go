package content

import "strings"

// maxProvenanceLines bounds how far into a document the provenance block
// may extend. sci writes seven fields; the cap is what makes "find the
// closing fence" safe on a paper that opens with a horizontal rule,
// since the search gives up long before it reaches the next one.
const maxProvenanceLines = 12

// provenanceMarker is the field sci writes first in every extraction
// note, and the positive evidence that a leading YAML block is ours.
const provenanceMarker = "zotero_key:"

// stripProvenance removes the YAML provenance block that sci's own
// extraction notes carry (see extract.MarkdownToNoteRaw), returning the
// paper's text alone.
//
// The block is metadata about the extraction — the parent item key, the
// PDF key, the title, the docling source, a hash and a date — and
// indexing it costs twice. It ruins snippets, because a query that
// matches the paper's title matches the block's `title:` line first and
// the excerpt shows YAML instead of prose. And it corrupts ranking, since
// every paper then contains its own title as body text, "docling" and
// "cached" match all 4,376 extractions, and the `generated:` date makes a
// search for a year hit everything indexed that year.
//
// Stripping is gated on positive evidence, the same discipline as
// notemd.IsHTMLNote: the text must open with a `---` fence, close it
// within [maxProvenanceLines], and carry [provenanceMarker] inside.
// Anything else — a paper that opens with a horizontal rule, a document
// with its own frontmatter, Zotero's plain-text cache — is returned
// unchanged. Guessing wrong here silently eats the top of a paper.
//
// The HTML note format (extract.MarkdownToNoteHTML, opt-in via --html)
// writes its provenance as a heading plus an italic line instead, which
// this does not detect. That is deliberate: it has no unambiguous marker,
// and in the live library essentially every extraction is markdown.
func stripProvenance(text string) string {
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
