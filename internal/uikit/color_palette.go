package uikit

// color_palette.go — Wong colorblind-safe palette shared across all TUI and
// CLI output. Colors are resolved once at init based on terminal background.

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Palette holds resolved colors for a specific light/dark mode.
type Palette struct {
	Blue   color.Color
	Orange color.Color
	Green  color.Color
	Red    color.Color
	Pink   color.Color

	TextBright    color.Color
	TextMid       color.Color
	TextDim       color.Color
	Surface       color.Color
	SurfaceRaised color.Color
	OnAccent      color.Color
	Border        color.Color
}

// NewPalette builds the Wong colorblind-safe palette for the given mode.
func NewPalette(isDark bool) Palette {
	ld := lipgloss.LightDark(isDark)
	return Palette{
		Blue:   ld(lipgloss.Color("#0072B2"), lipgloss.Color("#56B4E9")),
		Orange: ld(lipgloss.Color("#D55E00"), lipgloss.Color("#E69F00")),
		Green:  ld(lipgloss.Color("#007A5A"), lipgloss.Color("#009E73")),
		Red:    ld(lipgloss.Color("#CC3311"), lipgloss.Color("#D55E00")),
		Pink:   ld(lipgloss.Color("#AA4499"), lipgloss.Color("#CC79A7")),

		TextBright:    ld(lipgloss.Color("#1F2937"), lipgloss.Color("#E5E7EB")),
		TextMid:       ld(lipgloss.Color("#4B5563"), lipgloss.Color("#9CA3AF")),
		TextDim:       ld(lipgloss.Color("#6B7280"), lipgloss.Color("#6B7280")),
		Surface:       ld(lipgloss.Color("#F3F4F6"), lipgloss.Color("#1F2937")),
		SurfaceRaised: ld(lipgloss.Color("#E2E8F0"), lipgloss.Color("#2D3748")),
		OnAccent:      ld(lipgloss.Color("#FFFFFF"), lipgloss.Color("#0F172A")),
		Border:        ld(lipgloss.Color("#D1D5DB"), lipgloss.Color("#374151")),
	}
}

// DetectDark returns true if the terminal has a dark background.
// Falls back to true (dark) on error since most terminals are dark.
func DetectDark() bool {
	return lipgloss.HasDarkBackground(os.Stdin, os.Stderr)
}
