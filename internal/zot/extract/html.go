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
// and YAML frontmatter (for raw markdown mode).
type NoteMeta struct {
	// ParentKey is the Zotero item key of the parent item.
	ParentKey string
	// PDFKey is the Zotero item key of the attachment PDF (not the parent).
	PDFKey string
	// PDFName is the human-readable filename (Zotero auto-renames attachments
	// per user settings; we prefer the attachment item's `title` field).
	PDFName string
	// DOI is the parent item's DOI, if available. Empty string when absent.
	DOI string
	// Source identifies the converter + version, e.g. "docling 2.86.0".
	Source string
	// Hash is a short hex-encoded digest of the PDF bytes.
	Hash string
	// Generated is the wall-clock time at which the note was produced.
	Generated time.Time
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
func MarkdownToNoteHTML(md []byte, meta NoteMeta) string {
	cleaned := bytes.ReplaceAll(md, []byte("<!-- image -->"), []byte(figurePlaceholder))

	gm := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
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
	out.WriteString(" · fp:")
	out.WriteString(htmlEscape(meta.Hash))
	out.WriteString("</em></p>\n")
	out.WriteString("<hr>\n")
	out.WriteString(body.String())
	return out.String()
}

// MarkdownToNoteRaw renders the note body as raw markdown with YAML
// frontmatter. This is the default note format — better for LLM tools
// and search than rendered HTML.
func MarkdownToNoteRaw(md []byte, meta NoteMeta) string {
	var out strings.Builder
	out.WriteString("---\n")
	fmt.Fprintf(&out, "zotero_key: %s\n", meta.ParentKey)
	fmt.Fprintf(&out, "pdf_key: %s\n", meta.PDFKey)
	fmt.Fprintf(&out, "title: %q\n", meta.PDFName)
	if meta.DOI != "" {
		fmt.Fprintf(&out, "doi: %q\n", meta.DOI)
	}
	fmt.Fprintf(&out, "source: %s\n", meta.Source)
	fmt.Fprintf(&out, "hash: %s\n", meta.Hash)
	fmt.Fprintf(&out, "generated: %s\n", meta.Generated.UTC().Format("2006-01-02"))
	out.WriteString("---\n\n")
	out.Write(md)
	return out.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}
