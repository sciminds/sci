package app

// mode_styles.go — dbtui-specific modal editing styles.
//
// These styles are semantically tied to dbtui's normal/edit/visual mode
// paradigm. They use Tailwind-inspired color names (CursorBlue, SelectPink)
// rather than domain names (NormalCursor, VisualSelected) so the naming
// convention matches uikit.
//
// Shared styles come from uikit.TUI; only mode-specific cursor and header
// styles live here.

import (
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// modeTUI is the package-level singleton for dbtui mode styles.
var modeTUI = newModeStyles(uikit.DetectDark())

// modeStyles holds lipgloss styles specific to dbtui's modal editing modes.
type modeStyles struct {
	cursorRaised  lipgloss.Style
	cursorBlue    lipgloss.Style
	cursorOrange  lipgloss.Style
	cursorPink    lipgloss.Style
	selectPink    lipgloss.Style
	headerGreenBg lipgloss.Style
}

// newModeStyles creates a modeStyles instance for the given light/dark mode.
func newModeStyles(isDark bool) *modeStyles {
	p := uikit.NewPalette(isDark)
	ld := lipgloss.LightDark(isDark)

	return &modeStyles{
		cursorRaised: lipgloss.NewStyle().
			Background(p.SurfaceRaised).
			Bold(true),

		cursorBlue: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#BFDBFE"), lipgloss.Color("#1E3A5F"))).
			Foreground(p.Green).
			Bold(true),

		cursorOrange: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#FFF3E0"), lipgloss.Color("#2D1F0E"))).
			Foreground(p.TextBright).
			Bold(true).
			Underline(true),

		cursorPink: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#C4A8E0"), lipgloss.Color("#553970"))).
			Foreground(p.TextBright).
			Bold(true),

		selectPink: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#D8C8E8"), lipgloss.Color("#3D2B4D"))).
			Foreground(p.TextBright),

		headerGreenBg: lipgloss.NewStyle().
			Foreground(p.Green).
			Background(ld(lipgloss.Color("#E6F4EA"), lipgloss.Color("#1A3329"))).
			Bold(true),
	}
}

// CursorRaised returns the raised-surface cursor style.
func (s *modeStyles) CursorRaised() lipgloss.Style { return s.cursorRaised }

// CursorBlue returns the blue-tinted cursor style (normal mode).
func (s *modeStyles) CursorBlue() lipgloss.Style { return s.cursorBlue }

// CursorOrange returns the orange-tinted cursor style (edit mode).
func (s *modeStyles) CursorOrange() lipgloss.Style { return s.cursorOrange }

// CursorPink returns the pink-tinted cursor style (visual mode).
func (s *modeStyles) CursorPink() lipgloss.Style { return s.cursorPink }

// SelectPink returns the pink-tinted selection range style (visual mode).
func (s *modeStyles) SelectPink() lipgloss.Style { return s.selectPink }

// HeaderGreenBg returns the green-background column header style.
func (s *modeStyles) HeaderGreenBg() lipgloss.Style { return s.headerGreenBg }
