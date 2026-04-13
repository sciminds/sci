package uikit

import (
	"strings"

	"github.com/samber/lo"
)

// OverlayBox renders a modal-style overlay with a title section, body
// content, and hint footer. Each Hints entry is automatically styled
// with the shared HeaderHint style.
//
// Usage:
//
//	kit.OverlayBox{
//	    Title: "Player",
//	    Body:  player.View(),
//	    Hints: []string{"space pause/play", "r restart", "esc close"},
//	}.Render(m.width)
type OverlayBox struct {
	// Title is rendered as a HeaderSection label.
	Title string
	// Body is rendered as-is between title and footer.
	Body string
	// Hints are auto-styled with HeaderHint and joined with double-space.
	Hints []string
}

// Render returns the styled overlay box sized for the given terminal width.
func (o OverlayBox) Render(termW int) string {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)

	var b strings.Builder
	b.WriteString(TUI.HeaderSection().Render(" " + o.Title + " "))
	b.WriteString("\n\n")
	b.WriteString(o.Body)

	if len(o.Hints) > 0 {
		b.WriteString("\n\n")
		styled := lo.Map(o.Hints, func(h string, _ int) string {
			return TUI.HeaderHint().Render(h)
		})
		b.WriteString(strings.Join(styled, "  "))
	}

	return TUI.OverlayBox().
		Width(w).
		Render(b.String())
}
