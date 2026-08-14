// Package notemd reads the HTML that Zotero's note items store in their
// `note` field back out as markdown. Entry points:
//
//   - IsHTMLNote:     is this body real HTML, or markdown Zotero merely wrapped?
//   - HTMLToMarkdown: a Zotero note body rendered as markdown
//   - StripProvenance: drop the YAML header an extraction note opens with
//
// sci reads a Zotero library and never writes one, so this is a one-way
// street. Without it Zotero is an opaque store: sci can see that a
// literature note exists and never cleanly get it back out, which makes
// the library useless as a document source for anything downstream — an
// agent reading a note, a bibliography pass, an external index.
package notemd

import (
	stdhtml "html"
	"regexp"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
)

// toMarkdown is the HTML→markdown converter. Initialized once at package
// load.
//
// base + commonmark is deliberately the whole plugin set: it covers the
// vocabulary Zotero's own editor emits (headings, emphasis, code, lists,
// links, rules, blockquotes, tables) and nothing else, so the converter
// can't invent syntax for markup no note actually carries.
var toMarkdown = htmltomd.NewConverter(
	htmltomd.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(
			// A note whose source rule was "---" comes back as "---"
			// rather than "* * *".
			commonmark.WithHorizontalRule("---"),
		),
	),
)

var (
	// leadingDivRe matches the wrapper div(s) Zotero opens a note with:
	// `<div class="zotero-note znv1">` plus the editor's inner
	// `<div data-schema-version="9">`.
	leadingDivRe = regexp.MustCompile(`(?is)^\s*(<div[^>]*>\s*)+`)
	// anyDivRe matches every wrapper div tag, opening or closing.
	anyDivRe = regexp.MustCompile(`(?i)</?div[^>]*>`)
	// leadingBlockRe matches a block-level HTML tag at the very start of the
	// unwrapped body — the strongest signal that a note is HTML.
	leadingBlockRe = regexp.MustCompile(`(?i)^<(p|h[1-6]|ul|ol|blockquote|pre|table)[\s/>]`)
	// markdownBlockRe matches a markdown block construct at the start of a
	// line: frontmatter fence, ATX heading, list bullet, ordered item, or
	// code fence.
	markdownBlockRe = regexp.MustCompile("(?m)^(---[ \t]*$|#{1,6} |[-*+] |[0-9]+\\. |```)")
)

// IsHTMLNote reports whether a note body is genuinely HTML, as opposed to
// markdown that Zotero merely wrapped in a div.
//
// This distinction is not academic: in Eshin's library 5,098 of 5,140 notes
// are raw markdown inside the wrapper (the docling extraction path posts
// markdown by default; `--html` is opt-in). Running those through an
// HTML→markdown converter mangles them — newlines collapse, so the YAML
// provenance block at the top of every extraction note becomes one
// unparseable line, and underscores in `zotero_key` get backslash-escaped.
//
// HTML is the default answer — it is what Zotero declares the field to be —
// and a body escapes to "markdown" only on positive evidence. Two signals,
// in order:
//
//  1. A block-level tag opening the body means HTML, full stop. That is what
//     Zotero's editor writes.
//  2. Otherwise the body is markdown only if it carries a markdown block
//     construct (frontmatter fence, ATX heading, list, code fence).
//
// Testing what OPENS the body rather than what appears anywhere in it
// matters: docling markdown legitimately embeds HTML tables mid-document,
// and such a note is still markdown. Requiring positive markdown evidence
// matters too — a body of inline-only HTML ("<b>bold</b> and <i>italic</i>")
// opens with no block tag but is obviously not markdown.
//
// Residual gap: a plain-text note with no markdown structure and no tags
// takes the HTML path, where the converter backslash-escapes markdown
// punctuation. Harmless in practice — every extraction note carries a
// frontmatter fence, and hand-written notes come from Zotero's editor as
// real HTML.
func IsHTMLNote(noteHTML string) bool {
	body := strings.TrimSpace(leadingDivRe.ReplaceAllString(noteHTML, ""))
	if leadingBlockRe.MatchString(body) {
		return true
	}
	return !markdownBlockRe.MatchString(body)
}

// HTMLToMarkdown converts a Zotero note body back to markdown. Empty input
// returns "", nil.
//
// For a genuine HTML note this runs the converter; Zotero's wrapper divs
// carry no content and fall away. For a markdown note (the overwhelming
// majority — see [IsHTMLNote]) the body is already markdown, so the wrapper
// is stripped and entities are decoded and nothing else is touched. Anything
// more would be lossy.
func HTMLToMarkdown(noteHTML string) (string, error) {
	if noteHTML == "" {
		return "", nil
	}
	if !IsHTMLNote(noteHTML) {
		return strings.TrimSpace(stdhtml.UnescapeString(anyDivRe.ReplaceAllString(noteHTML, ""))), nil
	}
	out, err := toMarkdown.ConvertString(noteHTML)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
