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
//	}.Render(m.width, m.height)
type OverlayBox struct {
	// Title is rendered as a HeaderSection label.
	Title string
	// Body is rendered as-is between title and footer.
	Body string
	// Hints are auto-styled with HeaderHint and joined with double-space.
	Hints []string
}

// Render returns the styled overlay box sized for the given terminal width
// and height. If the body is taller than the available height, it is
// truncated with a trailing ellipsis line so the overlay never overflows the
// screen.
func (o OverlayBox) Render(termW, termH int) string {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)

	// Vertical chrome: box border + padding (4) + title (1) + blank (1) =
	// 6, plus blank (1) + hints (1) when hints are present.
	chrome := 6
	if len(o.Hints) > 0 {
		chrome += 2
	}
	body := truncateBody(o.Body, termH-chrome)

	var b strings.Builder
	b.WriteString(TUI.HeaderSection().Render(" " + o.Title + " "))
	b.WriteString("\n\n")
	b.WriteString(body)

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

// truncateBody clamps body to maxLines, replacing the last visible line with
// "…" when content is dropped. If termH is non-positive or the body fits
// already, the body is returned unchanged.
func truncateBody(body string, maxLines int) string {
	if maxLines < OverlayMinH {
		maxLines = OverlayMinH
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= maxLines {
		return body
	}
	lines = lines[:maxLines-1]
	lines = append(lines, "…")
	return strings.Join(lines, "\n")
}
