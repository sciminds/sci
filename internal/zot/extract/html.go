package extract

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// NoteMeta is the metadata block rendered into an extracted note's header
// and embedded into the sentinel comment used for dedupe / drift detection.
type NoteMeta struct {
	// PDFKey is the Zotero item key of the attachment PDF (not the parent).
	PDFKey string
	// PDFName is the human-readable filename (Zotero auto-renames attachments
	// per user settings; we prefer the attachment item's `title` field).
	PDFName string
	// Source identifies the converter + version, e.g. "docling 2.86.0".
	Source string
	// Hash is a short hex-encoded digest of the PDF bytes. Used both for
	// display in the header and for drift detection via the sentinel.
	Hash string
	// Generated is the wall-clock time at which the note was produced.
	Generated time.Time
}

// sentinelPrefix is the marker embedded in every extracted note's HTML.
// Format: `<!-- sci-extract:<pdfKey>:<hash> -->`. Any comment matching
// this prefix is treated as an sci-generated extraction.
const sentinelPrefix = "sci-extract:"

// sentinel returns the full HTML comment line for the given pdfKey + hash.
// Exposed via FindSentinel for parsing; emitting is internal to this file.
func sentinel(pdfKey, hash string) string {
	return fmt.Sprintf("<!-- %s%s:%s -->", sentinelPrefix, pdfKey, hash)
}

// figurePlaceholder is the markdown we substitute in for docling's
// `<!-- image -->` comments before goldmark runs. We use a markdown
// emphasis span so the goldmark pass turns it into `<em>(figure)</em>`
// — a visible marker that survives Zotero's note renderer (which
// strips HTML comments).
const figurePlaceholder = "*(figure)*"

// MarkdownToNoteHTML renders docling markdown to a Zotero-note-ready HTML
// string suitable for ItemData.Note.
//
// Transformations applied:
//   - `<!-- image -->` placeholders are replaced with *(figure)* so they
//     render as visible italic text.
//   - A header block (filename, source, date, short hash) is prepended.
//   - The dedupe sentinel is emitted right before the body separator so
//     PlanExtract can find it via FindSentinel.
func MarkdownToNoteHTML(md []byte, meta NoteMeta) string {
	cleaned := bytes.ReplaceAll(md, []byte("<!-- image -->"), []byte(figurePlaceholder))

	gm := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		// WithUnsafe trusts the input HTML — safe here because the only
		// raw HTML is what we produce above (header + sentinel).
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	var body bytes.Buffer
	_ = gm.Convert(cleaned, &body)

	var out strings.Builder
	out.WriteString("<h1>")
	out.WriteString(htmlEscape(meta.PDFName))
	out.WriteString("</h1>\n")
	out.WriteString("<p><em>")
	out.WriteString(htmlEscape(meta.Source))
	out.WriteString(" · ")
	out.WriteString(meta.Generated.UTC().Format("2006-01-02"))
	out.WriteString(" · sha256:")
	out.WriteString(htmlEscape(meta.Hash))
	out.WriteString("</em></p>\n")
	out.WriteString(sentinel(meta.PDFKey, meta.Hash))
	out.WriteString("\n<hr>\n")
	out.WriteString(body.String())
	return out.String()
}

// FindSentinel scans htmlBody for an sci-extract sentinel comment and
// returns the embedded pdfKey and hash. Returns ok=false if no sentinel
// is present or the payload is malformed (missing colon separator).
func FindSentinel(htmlBody string) (pdfKey, hash string, ok bool) {
	const open = "<!-- " + sentinelPrefix
	i := strings.Index(htmlBody, open)
	if i < 0 {
		return "", "", false
	}
	rest := htmlBody[i+len(open):]
	end := strings.Index(rest, " -->")
	if end < 0 {
		return "", "", false
	}
	payload := rest[:end]
	colon := strings.Index(payload, ":")
	if colon < 0 {
		return "", "", false
	}
	return payload[:colon], payload[colon+1:], true
}

// MarkdownToNoteRaw renders the same header/sentinel structure as
// MarkdownToNoteHTML, but wraps the markdown body in <pre> instead of
// converting it to HTML. This preserves the original formatting in
// Zotero's note viewer (monospaced, whitespace-preserved).
//
// Used by `zot item extract --save-md`.
func MarkdownToNoteRaw(md []byte, meta NoteMeta) string {
	var out strings.Builder
	out.WriteString("<h1>")
	out.WriteString(htmlEscape(meta.PDFName))
	out.WriteString("</h1>\n")
	out.WriteString("<p><em>")
	out.WriteString(htmlEscape(meta.Source))
	out.WriteString(" · ")
	out.WriteString(meta.Generated.UTC().Format("2006-01-02"))
	out.WriteString(" · sha256:")
	out.WriteString(htmlEscape(meta.Hash))
	out.WriteString(" · markdown")
	out.WriteString("</em></p>\n")
	out.WriteString(sentinel(meta.PDFKey, meta.Hash))
	out.WriteString("\n<hr>\n")
	out.WriteString("<pre>\n")
	out.WriteString(htmlEscape(string(md)))
	out.WriteString("</pre>\n")
	return out.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}
