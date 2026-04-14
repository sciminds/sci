// render.go — thin wrappers around uikit markdown rendering.
// The glamour engine, cache, and style detection live in uikit; mdview
// re-exports them under the original names for backward compatibility.

package mdview

import "github.com/sciminds/cli/internal/uikit"

// DetectStyle probes the terminal for dark/light background.
// Must be called before bubbletea takes over stdin.
func DetectStyle() { uikit.DetectTermStyle() }

// Render converts markdown to terminal-styled output at the given width.
// Results are cached so repeated calls with the same input are instant.
func Render(markdown string, width int) (string, error) {
	return uikit.RenderMarkdown(markdown, width)
}

// PreRender renders and caches multiple markdown documents at the given width.
func PreRender(docs []string, width int) { uikit.PreRenderMarkdown(docs, width) }

// HighlightMatches injects reverse-video ANSI escapes around case-insensitive
// matches of query within an already-styled (glamour) string.
func HighlightMatches(styled, query string) string {
	return uikit.HighlightMatches(styled, query)
}
