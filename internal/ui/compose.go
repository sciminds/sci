package ui

// compose.go — re-exports layout composition utilities from [compose] so
// callers within the sci CLI that already import "internal/ui" can use them
// without an extra import. The implementation lives in internal/tui/compose
// (pure lipgloss, no project deps) so standalone binaries like dbtui can
// share the same code.

import (
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/tui/compose"
)

// Spread renders left-aligned and right-aligned content within a fixed width.
// See [compose.Spread].
func Spread(width int, left, right string) string {
	return compose.Spread(width, left, right)
}

// SpreadMinGap is like [Spread] with a guaranteed minimum gap.
// See [compose.SpreadMinGap].
func SpreadMinGap(width, minGap int, left, right string) string {
	return compose.SpreadMinGap(width, minGap, left, right)
}

// Center centers s horizontally within width.
// See [compose.Center].
func Center(width int, s string) string {
	return compose.Center(width, s)
}

// PadRight pads s with trailing spaces to exactly width cells.
// See [compose.PadRight].
func PadRight(s string, width int) string {
	return compose.PadRight(s, width)
}

// PadLeft pads s with leading spaces to exactly width cells.
// See [compose.PadLeft].
func PadLeft(s string, width int) string {
	return compose.PadLeft(s, width)
}

// Pad pads s to exactly width cells, aligned by pos.
// See [compose.Pad].
func Pad(s string, width int, pos lipgloss.Position) string {
	return compose.Pad(s, width, pos)
}

// Fit truncates then pads s to exactly width cells.
// See [compose.Fit].
func Fit(s string, width int, pos lipgloss.Position) string {
	return compose.Fit(s, width, pos)
}

// FitRight is [Fit] with right-alignment.
// See [compose.FitRight].
func FitRight(s string, width int) string {
	return compose.FitRight(s, width)
}
