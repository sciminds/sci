package cliui

// chrome.go — shared TUI chrome: footer bars, horizontal dividers, and status
// row helpers used across all interactive views.

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// MaxDividerWidth is the maximum width for horizontal dividers in TUI views.
const MaxDividerWidth = 60

// FooterBar renders a bottom bar with left-aligned and right-aligned content,
// filling the gap with spaces. If width is 0 or the content exceeds the width,
// only the left side is returned.
//
// Thin wrapper around [Spread] retained for readability at call sites.
func FooterBar(left, right string, width int) string {
	return uikit.Spread(width, left, right)
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
			s = uikit.TUI.TextGreenBold().Render(text)
		case SummaryDanger:
			s = uikit.TUI.TextRedBold().Render(text)
		case SummaryDim:
			s = uikit.TUI.Dim().Render(text)
		}
		rendered = append(rendered, s)
	}
	sep := uikit.TUI.TextPink().Render(" · ")
	return strings.Join(rendered, sep)
}

// StatusRow renders a standard indented icon + label line used in phase views.
func StatusRow(icon, label string) string {
	return fmt.Sprintf("  %s %s", icon, label)
}

// PageLayout composes a standard TUI page: title header, body, and footer bar,
// all wrapped in the shared Page() style.
func PageLayout(title, body, footerLeft, footerRight string, width int) string {
	header := uikit.TUI.Title().Render(title)
	left := uikit.TUI.Footer().Render(footerLeft)
	right := footerRight
	bottomBar := FooterBar(left, right, uikit.ContentWidth(width))

	page := fmt.Sprintf("%s\n\n%s\n\n%s", header, body, bottomBar)
	return uikit.TUI.Page().Render(page)
}
