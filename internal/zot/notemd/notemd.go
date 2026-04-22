// Package notemd renders markdown or raw HTML into the sanitized HTML that
// Zotero's note items store in their `note` field. Single entry points:
//
//   - MarkdownToHTML: goldmark-render + bluemonday-sanitize
//   - SanitizeHTML:   bluemonday-sanitize only (for --html passthrough)
//
// Both share one sanitizer policy so the allow-list is identical across the
// markdown and raw-HTML paths — what you can write as HTML directly is
// exactly what markdown output is allowed to produce.
package notemd

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// md renders CommonMark + GFM (tables, strikethrough, task lists,
// autolinks) into HTML. Initialized once at package load.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithXHTML()),
)

// policy is the shared sanitizer. UGCPolicy covers everything lab notes
// want (headings, lists, code, links, tables, blockquotes) while stripping
// scripts, iframes, event handlers, and unknown attributes.
//
// `zotero` is allowed alongside the UGC defaults (http/https/mailto) so
// `zotero://select/…` cross-links in summary notes survive rendering —
// Zotero's desktop app resolves these to an item/collection select action.
var policy = bluemonday.UGCPolicy().
	AllowURLSchemes("http", "https", "mailto", "zotero")

// MarkdownToHTML parses src as CommonMark, renders to HTML, and sanitizes
// the result with the package policy. Empty input returns "", nil.
func MarkdownToHTML(src []byte) (string, error) {
	if len(src) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return "", err
	}
	return policy.Sanitize(buf.String()), nil
}

// SanitizeHTML runs src through the package policy. Intended for the
// `--html` passthrough path — callers who want to write literal HTML still
// get the same tag/attribute allow-list as the markdown path.
func SanitizeHTML(src string) string {
	if src == "" {
		return ""
	}
	return policy.Sanitize(src)
}
