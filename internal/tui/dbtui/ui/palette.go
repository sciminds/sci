package ui

// palette.go — Wong colorblind-safe palette shared across all dbtui TUI
// output. Colors are resolved once at init based on terminal background.

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette holds resolved colors for a specific light/dark mode.
type Palette struct {
	Accent    color.Color
	Secondary color.Color
	Success   color.Color
	Danger    color.Color
	Muted     color.Color

	TextBright color.Color
	TextMid    color.Color
	TextDim    color.Color
	Surface    color.Color
	OnAccent   color.Color
	Border     color.Color
}

// NewPalette builds the Wong colorblind-safe palette for the given mode.
// NOTE: dbtui's palette values differ from the main ui palette (e.g. Surface).
func NewPalette(isDark bool) Palette {
	ld := lipgloss.LightDark(isDark)
	return Palette{
		Accent:    ld(lipgloss.Color("#0072B2"), lipgloss.Color("#56B4E9")),
		Secondary: ld(lipgloss.Color("#D55E00"), lipgloss.Color("#E69F00")),
		Success:   ld(lipgloss.Color("#007A5A"), lipgloss.Color("#009E73")),
		Danger:    ld(lipgloss.Color("#CC3311"), lipgloss.Color("#D55E00")),
		Muted:     ld(lipgloss.Color("#AA4499"), lipgloss.Color("#CC79A7")),

		TextBright: ld(lipgloss.Color("#1F2937"), lipgloss.Color("#E5E7EB")),
		TextMid:    ld(lipgloss.Color("#4B5563"), lipgloss.Color("#9CA3AF")),
		TextDim:    ld(lipgloss.Color("#6B7280"), lipgloss.Color("#6B7280")),
		Surface:    ld(lipgloss.Color("#E2E8F0"), lipgloss.Color("#2D3748")),
		OnAccent:   ld(lipgloss.Color("#FFFFFF"), lipgloss.Color("#0F172A")),
		Border:     ld(lipgloss.Color("#D1D5DB"), lipgloss.Color("#374151")),
	}
}
