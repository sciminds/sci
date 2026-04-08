package ui

// palette.go — Wong colorblind-safe palette shared across all TUI and CLI
// output. Colors are resolved once at init based on terminal background.

import (
	"image/color"
	"os"

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
		Surface:    ld(lipgloss.Color("#F3F4F6"), lipgloss.Color("#1F2937")),
		OnAccent:   ld(lipgloss.Color("#FFFFFF"), lipgloss.Color("#0F172A")),
		Border:     ld(lipgloss.Color("#D1D5DB"), lipgloss.Color("#374151")),
	}
}

// DetectDark returns true if the terminal has a dark background.
// Falls back to true (dark) on error since most terminals are dark.
func DetectDark() bool {
	return lipgloss.HasDarkBackground(os.Stdin, os.Stderr)
}

// ── Pre-rendered symbols for non-TUI CLI output ─────────────────────────────

var (
	SymOK    = TUI.Pass().Render(IconPass)
	SymFail  = TUI.Fail().Render(IconFail)
	SymWarn  = TUI.Warn().Render(IconWarn)
	SymArrow = TUI.Accent().Render(IconArrow)
)
