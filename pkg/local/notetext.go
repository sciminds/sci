package local

import (
	"html"
	"regexp"
	"strings"
)

// tagRe matches an HTML tag. Zotero stores note bodies as HTML, so every
// note is wrapped in markup that a naive text search would happily match.
var tagRe = regexp.MustCompile(`<[^>]*>`)

// NoteText renders a Zotero note's stored HTML down to searchable plain
// text: tags become spaces and entities are decoded.
//
// Tags become a space rather than nothing so `<h1>Title</h1><p>Body` can't
// weld "TitleBody" into a token that matches queries spanning neither word.
//
// This is the indexing path for extraction notes (see
// internal/zot/content). Without it every note's wrapper —
// `<div class="zotero-note znv1">` — would make "div", "class" and "znv1"
// searchable terms present on every paper in the library.
//
// Not a display renderer: for that, notemd.HTMLToMarkdown preserves
// headings, lists and links that this deliberately flattens.
func NoteText(noteHTML string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(noteHTML, " "))), " ")
}
