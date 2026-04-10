package ui

// chrome.go — shared TUI chrome: footer bars, horizontal dividers, and status
// row helpers used across all interactive views.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// MaxDividerWidth is the maximum width for horizontal dividers in TUI views.
const MaxDividerWidth = 60

// FooterBar renders a bottom bar with left-aligned and right-aligned content,
// filling the gap with spaces. If width is 0 or the content exceeds the width,
// only the left side is returned.
func FooterBar(left, right string, width int) string {
	if width <= 0 {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap <= 0 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// SummaryKind controls how a summary part is styled.
type SummaryKind int

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
			s = TUI.SuccessBold().Render(text)
		case SummaryDanger:
			s = TUI.DangerBold().Render(text)
		case SummaryDim:
			s = TUI.Dim().Render(text)
		}
		rendered = append(rendered, s)
	}
	sep := TUI.Muted().Render(" · ")
	return strings.Join(rendered, sep)
}

// StatusRow renders a standard indented icon + label line used in phase views.
func StatusRow(icon, label string) string {
	return fmt.Sprintf("  %s %s", icon, label)
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
