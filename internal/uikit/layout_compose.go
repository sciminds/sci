// layout_compose.go — declarative layout composition utilities built on lipgloss.

package uikit

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── Spread: left + right in a fixed width ──────────────────────────────────

// Spread renders left-aligned and right-aligned content within a fixed width,
// filling the gap with spaces. If the combined content exceeds width, right is
// dropped entirely.
//
//	|left                           right|
func Spread(width int, left, right string) string {
	if width <= 0 {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap <= 0 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// SpreadMinGap is like [Spread] but guarantees at least minGap spaces between
// left and right. If content is too wide even with the minimum gap, left is
// truncated to make room for right.
//
//	|left              ·minGap·     right|
func SpreadMinGap(width, minGap int, left, right string) string {
	if width <= 0 {
		return left
	}
	if minGap < 1 {
		minGap = 1
	}
	rw := lipgloss.Width(right)
	lw := lipgloss.Width(left)
	gap := width - lw - rw
	if gap >= minGap {
		return left + strings.Repeat(" ", gap) + right
	}
	// Truncate left to fit.
	maxLeft := width - rw - minGap
	if maxLeft < 1 {
		return left // can't fit right at all
	}
	left = ansi.Truncate(left, maxLeft, "")
	lw = lipgloss.Width(left)
	gap = width - lw - rw
	if gap < minGap {
		gap = minGap
	}
	return left + strings.Repeat(" ", gap) + right
}

// ── Center: center content in a width ──────────────────────────────────────

// Center centers s horizontally within width using space padding. If s is
// already wider than width it is returned unchanged.
func Center(width int, s string) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, s)
}

// ── Pad: pad content to a fixed width with alignment ───────────────────────

// PadRight pads s with trailing spaces to exactly width cells. If s is already
// at least width cells wide it is returned unchanged (no truncation).
func PadRight(s string, width int) string {
	sw := lipgloss.Width(s)
	if sw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-sw)
}

// PadLeft pads s with leading spaces to exactly width cells. If s is already
// at least width cells wide it is returned unchanged (no truncation).
func PadLeft(s string, width int) string {
	sw := lipgloss.Width(s)
	if sw >= width {
		return s
	}
	return strings.Repeat(" ", width-sw) + s
}

// Pad pads s to exactly width cells, aligned by pos (Left, Center, Right).
// Content wider than width is returned unchanged (no truncation).
func Pad(s string, width int, pos lipgloss.Position) string {
	switch pos {
	case lipgloss.Right:
		return PadLeft(s, width)
	case lipgloss.Center:
		return Center(width, s)
	default:
		return PadRight(s, width)
	}
}

// ── Fit: truncate + pad in one step ────────────────────────────────────────

// Fit truncates s to width cells (with ellipsis) then pads to exactly width.
// This is the standard "fill a column" operation for table cells.
func Fit(s string, width int, pos lipgloss.Position) string {
	if width < 1 {
		return ""
	}
	truncated := ansi.Truncate(s, width, "\u2026")
	return Pad(truncated, width, pos)
}

// FitRight is [Fit] with right-alignment (numeric columns).
func FitRight(s string, width int) string {
	return Fit(s, width, lipgloss.Right)
}

// ── FitHeight: pad or truncate to exact row count ─────────────────────────

// FitHeight pads or truncates s so it contains exactly h newline-
// delimited lines. Useful for any region that must fill an exact number
// of rows (e.g. Chrome body, viewport panes).
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

// ── WordWrap: paragraph-aware word wrapping ────────────────────────────────

// WordWrap wraps text at maxW, preserving paragraph breaks (newlines).
func WordWrap(text string, maxW int) string {
	if maxW <= 0 || text == "" {
		return text
	}
	var result strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
		if result.Len() > 0 {
			result.WriteByte('\n')
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			continue
		}
		lineW := 0
		for i, word := range words {
			ww := lipgloss.Width(word)
			if i == 0 {
				result.WriteString(word)
				lineW = ww
				continue
			}
			if lineW+1+ww > maxW {
				result.WriteByte('\n')
				result.WriteString(word)
				lineW = ww
			} else {
				result.WriteByte(' ')
				result.WriteString(word)
				lineW += 1 + ww
			}
		}
	}
	return result.String()
}

// ── Page layout helpers ───────────────────────────────────────────────────

// MaxDividerWidth is the maximum width for horizontal dividers in TUI views.
const MaxDividerWidth = 60

// FooterBar renders a bottom bar with left-aligned and right-aligned content,
// filling the gap with spaces. Thin wrapper around [Spread] retained for
// readability at call sites.
func FooterBar(left, right string, width int) string {
	return Spread(width, left, right)
}

// PageLayout composes a standard TUI page: title header, body, and footer bar,
// all wrapped in the shared Page() style.
func PageLayout(title, body, footerLeft, footerRight string, width int) string {
	header := TUI.Title().Render(title)
	left := TUI.Footer().Render(footerLeft)
	right := footerRight
	bottomBar := FooterBar(left, right, ContentWidth(width))

	page := fmt.Sprintf("%s\n\n%s\n\n%s", header, body, bottomBar)
	return TUI.Page().Render(page)
}

// StatusRow renders a standard indented icon + label line used in phase views.
func StatusRow(icon, label string) string {
	return fmt.Sprintf("  %s %s", icon, label)
}

// SummaryKind controls how a summary part is styled.
type SummaryKind int

// SummaryKind constants for styling summary line segments.
const (
	SummarySuccess SummaryKind = iota // green bold
	SummaryDanger                     // red bold
	SummaryDim                        // faint
)

// SummaryPart is one segment of a summary line (e.g. "3 passed").
type SummaryPart struct {
	Count int
	Label string
	Kind  SummaryKind
}

// SummaryLine renders a "N label · N label · …" summary. Zero-count parts
// are omitted. The first part is always shown (even at zero) and left-padded.
func SummaryLine(parts ...SummaryPart) string {
	var rendered []string
	for i, p := range parts {
		if i > 0 && p.Count == 0 {
			continue
		}
		text := fmt.Sprintf("%d %s", p.Count, p.Label)
		if i == 0 {
			text = "  " + text
		}
		var s string
		switch p.Kind {
		case SummarySuccess:
			s = TUI.TextGreenBold().Render(text)
		case SummaryDanger:
			s = TUI.TextRedBold().Render(text)
		case SummaryDim:
			s = TUI.Dim().Render(text)
		}
		rendered = append(rendered, s)
	}
	sep := TUI.TextPink().Render(" · ")
	return strings.Join(rendered, sep)
}
