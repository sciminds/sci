package kit

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Chrome renders a three-part vertical layout: title bar, body, and
// status bar — the standard TUI "chrome" that wraps every screen.
//
// The body callback receives the remaining height after title and status
// are measured, so callers never need to compute chrome offsets manually.
// The result is always exactly height lines tall.
type Chrome struct {
	// Title renders the title bar. Receives available width.
	Title func(width int) string
	// Status renders the status bar. Receives available width.
	Status func(width int) string
	// Body renders the main content area. Receives available width and
	// the remaining height after title and status are measured.
	Body func(width, height int) string
}

// Render composes the three sections vertically, padding or truncating
// the body so the total output is exactly height lines tall.
func (c Chrome) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	title := c.Title(width)
	status := c.Status(width)

	titleH := lipgloss.Height(title)
	statusH := lipgloss.Height(status)
	bodyH := max(1, height-titleH-statusH)

	body := c.Body(width, bodyH)
	body = FitHeight(body, bodyH)

	return lipgloss.JoinVertical(lipgloss.Left, title, body, status)
}

// FitHeight pads or truncates s so it contains exactly h newline-
// delimited lines. Useful outside Chrome for any region that must fill
// an exact number of rows.
func FitHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
