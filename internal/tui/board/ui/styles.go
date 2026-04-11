// Package ui holds the lipgloss styles for the board TUI.
//
// Everything visual lives here: edit this file to adjust spacing, borders,
// palette assignments, etc. The app/ package reads styles via the TUI
// singleton only — no inline lipgloss.NewStyle calls in view code.
package ui

import (
	"charm.land/lipgloss/v2"
	parentui "github.com/sciminds/cli/internal/ui"
)

// Layout constants — single source of truth for spacing and sizing.
// Tweak these to adjust board density without touching any view code.
const (
	// ColumnWidth is the target width of a single kanban column in cells.
	// The grid renderer divides available width among columns but clamps
	// to this width when the terminal is wide.
	ColumnWidth = 28

	// ColumnGap is the horizontal gap between adjacent columns.
	ColumnGap = 1

	// CardPaddingX / CardPaddingY are the padding inside a card frame
	// (interior of the card border).
	CardPaddingX = 1
	CardPaddingY = 0

	// CardGap is the vertical gap between stacked cards in a column.
	CardGap = 1

	// ChromeLines reserved for the title bar + status bar.
	TitleLines  = 1
	StatusLines = 1
)

// Styles holds all lipgloss styles the board TUI uses. One place to
// tweak every visual aspect of the board.
type Styles struct {
	palette parentui.Palette

	// Chrome ─────────────────────────────────────────────────────────
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Status    lipgloss.Style
	StatusErr lipgloss.Style
	Help      lipgloss.Style
	Keycap    lipgloss.Style

	// Picker ─────────────────────────────────────────────────────────
	PickerItem     lipgloss.Style
	PickerSelected lipgloss.Style
	PickerHint     lipgloss.Style

	// Columns ────────────────────────────────────────────────────────
	ColumnFrame lipgloss.Style
	ColumnFocus lipgloss.Style
	ColumnTitle lipgloss.Style
	ColumnCount lipgloss.Style

	// Cards ──────────────────────────────────────────────────────────
	Card         lipgloss.Style
	CardSelected lipgloss.Style
	CardTitle    lipgloss.Style
	CardMeta     lipgloss.Style
	CardLabel    lipgloss.Style
	CardPriority lipgloss.Style

	// Detail ─────────────────────────────────────────────────────────
	DetailFrame   lipgloss.Style
	DetailHeading lipgloss.Style
	DetailBody    lipgloss.Style
	DetailSection lipgloss.Style
}

// TUI is the package-level shared styles singleton. Reach for it from
// the app package via ui.TUI.
var TUI = New(parentui.DetectDark())

// New builds a Styles instance for the given dark-mode preference.
// Callers should not normally use this directly — use TUI.
func New(isDark bool) *Styles {
	p := parentui.NewPalette(isDark)
	border := lipgloss.RoundedBorder()
	cardBorder := lipgloss.RoundedBorder()

	return &Styles{
		palette: p,

		Title: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Padding(0, 1),
		Subtitle: lipgloss.NewStyle().
			Foreground(p.TextMid),
		Status: lipgloss.NewStyle().
			Foreground(p.TextMid).
			Padding(0, 1),
		StatusErr: lipgloss.NewStyle().
			Foreground(p.Danger).
			Padding(0, 1),
		Help: lipgloss.NewStyle().
			Foreground(p.TextDim),
		Keycap: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.TextBright).
			Padding(0, 1).
			Bold(true),

		PickerItem: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Padding(0, 2),
		PickerSelected: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Accent).
			Padding(0, 2).
			Bold(true),
		PickerHint: lipgloss.NewStyle().
			Foreground(p.TextDim).
			Italic(true).
			Padding(0, 2),

		ColumnFrame: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.TextDim).
			Padding(0, 1),
		ColumnFocus: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.Accent).
			Padding(0, 1),
		ColumnTitle: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Underline(true),
		ColumnCount: lipgloss.NewStyle().
			Foreground(p.TextDim).
			Bold(true),

		Card: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Border(cardBorder).
			BorderForeground(p.Border).
			Padding(CardPaddingY, CardPaddingX),
		CardSelected: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Border(cardBorder).
			BorderForeground(p.Accent).
			Padding(CardPaddingY, CardPaddingX).
			Bold(true),
		CardTitle: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Bold(true),
		CardMeta: lipgloss.NewStyle().
			Foreground(p.TextDim),
		CardLabel: lipgloss.NewStyle().
			Foreground(p.Secondary),
		CardPriority: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Bold(true),

		DetailFrame: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.Accent).
			Padding(1, 2),
		DetailHeading: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true),
		DetailBody: lipgloss.NewStyle().
			Foreground(p.TextMid),
		DetailSection: lipgloss.NewStyle().
			Foreground(p.TextDim).
			Bold(true),
	}
}

// Palette exposes the underlying palette for callers that need raw colors.
func (s *Styles) Palette() parentui.Palette { return s.palette }
