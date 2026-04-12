package ui

// styles.go — the single source of truth for all lipgloss styles.

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Styles holds pre-built lipgloss styles shared across all TUI commands.
type Styles struct {
	// palette is retained so callers that need raw colors can access them.
	palette Palette

	// ── Semantic foreground ──────────────────────────────────────────────

	pass   lipgloss.Style
	fail   lipgloss.Style
	warn   lipgloss.Style
	accent lipgloss.Style
	muted  lipgloss.Style
	dim    lipgloss.Style
	dimIt  lipgloss.Style
	bold   lipgloss.Style

	// Fine-grained foreground (used heavily by db TUI)
	fgTextDim    lipgloss.Style
	fgTextMid    lipgloss.Style
	fgTextBright lipgloss.Style
	fgAccent     lipgloss.Style
	fgSecondary  lipgloss.Style
	fgSuccess    lipgloss.Style
	fgDanger     lipgloss.Style
	fgMuted      lipgloss.Style
	fgBorder     lipgloss.Style

	// Bold / italic variants
	fgTextDimBold     lipgloss.Style
	fgTextDimItalic   lipgloss.Style
	fgAccentBold      lipgloss.Style
	fgAccentItalic    lipgloss.Style
	fgSecondaryBold   lipgloss.Style
	fgSecondaryItalic lipgloss.Style
	fgSuccessBold     lipgloss.Style
	fgMutedBold       lipgloss.Style
	fgDangerBold      lipgloss.Style
	colActiveHeader   lipgloss.Style

	// ── Layout / chrome ─────────────────────────────────────────────────

	heading lipgloss.Style
	cursor  lipgloss.Style
	title   lipgloss.Style
	footer  lipgloss.Style
	panel   lipgloss.Style
	page    lipgloss.Style
	divider lipgloss.Style
	keycap  lipgloss.Style

	// Composite / badge styles
	base            lipgloss.Style
	accentPill      lipgloss.Style
	headerSection   lipgloss.Style
	tabInactive     lipgloss.Style
	tabLocked       lipgloss.Style
	tableSelected   lipgloss.Style
	modeEdit        lipgloss.Style
	modeVisualBadge lipgloss.Style
	normalCursor    lipgloss.Style
	editCursor      lipgloss.Style
	visualSelected  lipgloss.Style
	visualCursor    lipgloss.Style
	overlayBox      lipgloss.Style

	// Help rendering
	helpDesc    lipgloss.Style
	helpSection lipgloss.Style
	helpHint    lipgloss.Style
	helpUsage   lipgloss.Style
}

// TUI is the package-level shared styles singleton.
var TUI = NewStyles(lipgloss.HasDarkBackground(os.Stdin, os.Stderr))

// NewStyles creates a Styles instance for the given light/dark mode.
func NewStyles(isDark bool) *Styles {
	p := NewPalette(isDark)
	ld := lipgloss.LightDark(isDark)

	return &Styles{
		palette: p,

		// Semantic foreground
		pass:   lipgloss.NewStyle().Foreground(p.Success),
		fail:   lipgloss.NewStyle().Foreground(p.Danger),
		warn:   lipgloss.NewStyle().Foreground(p.Secondary),
		accent: lipgloss.NewStyle().Foreground(p.Accent),
		muted:  lipgloss.NewStyle().Foreground(p.Muted),
		dim:    lipgloss.NewStyle().Faint(true),
		dimIt:  lipgloss.NewStyle().Faint(true).Italic(true),
		bold:   lipgloss.NewStyle().Bold(true),

		// Fine-grained foreground
		fgTextDim:    lipgloss.NewStyle().Foreground(p.TextDim),
		fgTextMid:    lipgloss.NewStyle().Foreground(p.TextMid),
		fgTextBright: lipgloss.NewStyle().Foreground(p.TextBright),
		fgAccent:     lipgloss.NewStyle().Foreground(p.Accent),
		fgSecondary:  lipgloss.NewStyle().Foreground(p.Secondary),
		fgSuccess:    lipgloss.NewStyle().Foreground(p.Success),
		fgDanger:     lipgloss.NewStyle().Foreground(p.Danger),
		fgMuted:      lipgloss.NewStyle().Foreground(p.Muted),
		fgBorder:     lipgloss.NewStyle().Foreground(p.Border),

		// Bold / italic variants
		fgTextDimBold:     lipgloss.NewStyle().Foreground(p.TextDim).Bold(true),
		fgTextDimItalic:   lipgloss.NewStyle().Foreground(p.TextDim).Italic(true),
		fgAccentBold:      lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		fgAccentItalic:    lipgloss.NewStyle().Foreground(p.Accent).Italic(true),
		fgSecondaryBold:   lipgloss.NewStyle().Foreground(p.Secondary).Bold(true),
		fgSecondaryItalic: lipgloss.NewStyle().Foreground(p.Secondary).Italic(true),
		fgSuccessBold:     lipgloss.NewStyle().Foreground(p.Success).Bold(true),
		colActiveHeader: lipgloss.NewStyle().
			Foreground(p.Success).
			Background(ld(lipgloss.Color("#E6F4EA"), lipgloss.Color("#1A3329"))).
			Bold(true),
		fgMutedBold:  lipgloss.NewStyle().Foreground(p.Muted).Bold(true),
		fgDangerBold: lipgloss.NewStyle().Foreground(p.Danger).Bold(true),

		// Layout / chrome
		heading: lipgloss.NewStyle().Foreground(p.TextBright).Bold(true),
		cursor:  lipgloss.NewStyle().Background(p.Surface).Bold(true),
		title: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Foreground(p.TextMid).
			Padding(0, 1),
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Muted).
			Padding(0, 1),
		page: lipgloss.NewStyle().
			Padding(1, 2),
		divider: lipgloss.NewStyle().
			Foreground(p.TextDim),
		keycap: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.TextBright).
			Padding(0, 1).
			Bold(true),

		// Composite / badge styles
		base: lipgloss.NewStyle(),
		accentPill: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Accent).
			Padding(0, 1).
			Bold(true),
		headerSection: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Background(p.Border).
			Padding(0, 1).
			Bold(true),
		tabInactive: lipgloss.NewStyle().
			Foreground(p.TextMid).
			Padding(0, 1),
		tabLocked: lipgloss.NewStyle().
			Foreground(p.TextDim).
			Padding(0, 1).
			Strikethrough(true),
		tableSelected: lipgloss.NewStyle().
			Background(p.Surface).
			Bold(true),
		normalCursor: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#BFDBFE"), lipgloss.Color("#1E3A5F"))).
			Foreground(p.Success).
			Bold(true),
		editCursor: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#FFF3E0"), lipgloss.Color("#2D1F0E"))).
			Foreground(p.TextBright).
			Bold(true).
			Underline(true),
		modeEdit: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Secondary).
			Padding(0, 1).
			Bold(true),
		modeVisualBadge: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Muted).
			Padding(0, 1).
			Bold(true),
		visualSelected: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#D8C8E8"), lipgloss.Color("#3D2B4D"))).
			Foreground(p.TextBright),
		visualCursor: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#C4A8E0"), lipgloss.Color("#553970"))).
			Foreground(p.TextBright).
			Bold(true),
		overlayBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Accent).
			Padding(1, 2),

		// Help rendering
		helpDesc: lipgloss.NewStyle().
			Foreground(p.TextMid).
			PaddingLeft(2),
		helpSection: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Bold(true).
			PaddingLeft(2),
		helpHint: lipgloss.NewStyle().
			Faint(true).
			Italic(true).
			PaddingLeft(2),
		helpUsage: lipgloss.NewStyle().
			Foreground(p.TextBright).
			Bold(true),
	}
}

// Palette returns the resolved color palette backing this Styles instance.
func (s *Styles) Palette() Palette { return s.palette }

// ── Accessors — semantic foreground ────────────────────────────────────────
//
// These return single-property styles for coloring text. Compose with
// .Render("text") or chain with .Bold(true) etc. for one-off variations.

// Pass returns the success/pass foreground style (green).
func (s *Styles) Pass() lipgloss.Style { return s.pass }

// Fail returns the failure/error foreground style (red).
func (s *Styles) Fail() lipgloss.Style { return s.fail }

// Warn returns the warning foreground style (orange).
func (s *Styles) Warn() lipgloss.Style { return s.warn }

// Accent returns the primary accent foreground style (blue).
func (s *Styles) Accent() lipgloss.Style { return s.accent }

// Muted returns the de-emphasized foreground style (purple).
func (s *Styles) Muted() lipgloss.Style { return s.muted }

// Dim returns a faint (low opacity) style for background elements.
func (s *Styles) Dim() lipgloss.Style { return s.dim }

// DimIt returns a faint + italic style.
func (s *Styles) DimIt() lipgloss.Style { return s.dimIt }

// Bold returns a bold style with no color override.
func (s *Styles) Bold() lipgloss.Style { return s.bold }

// Heading returns the section heading style (bright + bold).
func (s *Styles) Heading() lipgloss.Style { return s.heading }

// Cursor returns the cursor background highlight style.
func (s *Styles) Cursor() lipgloss.Style { return s.cursor }

// Title returns the title style (accent + bold + padded).
func (s *Styles) Title() lipgloss.Style { return s.title }

// Footer returns the footer text style (mid-contrast + padded).
func (s *Styles) Footer() lipgloss.Style { return s.footer }

// Panel returns a rounded-border panel style for content boxes.
func (s *Styles) Panel() lipgloss.Style { return s.panel }

// Page returns a padded page container style.
func (s *Styles) Page() lipgloss.Style { return s.page }

// Divider returns the horizontal rule / divider style.
func (s *Styles) Divider() lipgloss.Style { return s.divider }

// Keycap returns the keyboard shortcut badge style (inverted pill).
func (s *Styles) Keycap() lipgloss.Style { return s.keycap }

// ── Accessors — fine-grained foreground ─────────────────────────────────────

// TextDim returns the dim foreground style.
func (s *Styles) TextDim() lipgloss.Style { return s.fgTextDim }

// TextMid returns the mid-brightness foreground style.
func (s *Styles) TextMid() lipgloss.Style { return s.fgTextMid }

// TextBright returns the bright foreground style.
func (s *Styles) TextBright() lipgloss.Style { return s.fgTextBright }

// FgAccent returns the accent foreground style.
func (s *Styles) FgAccent() lipgloss.Style { return s.fgAccent }

// FgSecondary returns the secondary foreground style.
func (s *Styles) FgSecondary() lipgloss.Style { return s.fgSecondary }

// FgSuccess returns the success foreground style.
func (s *Styles) FgSuccess() lipgloss.Style { return s.fgSuccess }

// FgDanger returns the danger foreground style.
func (s *Styles) FgDanger() lipgloss.Style { return s.fgDanger }

// FgMuted returns the muted foreground style.
func (s *Styles) FgMuted() lipgloss.Style { return s.fgMuted }

// FgBorder returns the border foreground style.
func (s *Styles) FgBorder() lipgloss.Style { return s.fgBorder }

// ── Accessors — bold/italic variants ────────────────────────────────────────

// AccentBold returns the bold accent style.
func (s *Styles) AccentBold() lipgloss.Style { return s.fgAccentBold }

// AccentItalic returns the italic accent style.
func (s *Styles) AccentItalic() lipgloss.Style { return s.fgAccentItalic }

// SecondaryBold returns the bold secondary style.
func (s *Styles) SecondaryBold() lipgloss.Style { return s.fgSecondaryBold }

// SecondaryItalic returns the italic secondary style.
func (s *Styles) SecondaryItalic() lipgloss.Style { return s.fgSecondaryItalic }

// SuccessBold returns the bold success style.
func (s *Styles) SuccessBold() lipgloss.Style { return s.fgSuccessBold }

// MutedBold returns the bold muted style.
func (s *Styles) MutedBold() lipgloss.Style { return s.fgMutedBold }

// DangerBold returns the bold danger style.
func (s *Styles) DangerBold() lipgloss.Style { return s.fgDangerBold }

// TextDimBold returns the bold dim text style.
func (s *Styles) TextDimBold() lipgloss.Style { return s.fgTextDimBold }

// TextDimItalic returns the italic dim text style.
func (s *Styles) TextDimItalic() lipgloss.Style { return s.fgTextDimItalic }

// ── Accessors — semantic aliases (db TUI) ───────────────────────────────────

// Readonly returns the read-only cell style.
func (s *Styles) Readonly() lipgloss.Style { return s.fgTextDim }

// Empty returns the empty cell style.
func (s *Styles) Empty() lipgloss.Style { return s.fgTextDim }

// CellDim returns the dimmed cell style.
func (s *Styles) CellDim() lipgloss.Style { return s.fgTextDim }

// HeaderHint returns the header hint style.
func (s *Styles) HeaderHint() lipgloss.Style { return s.fgTextMid }

// TabUnderline returns the tab underline style.
func (s *Styles) TabUnderline() lipgloss.Style { return s.fgAccent }

// AccentText returns the accent text style.
func (s *Styles) AccentText() lipgloss.Style { return s.fgAccent }

// SortArrow returns the sort arrow style.
func (s *Styles) SortArrow() lipgloss.Style { return s.fgSecondary }

// SecondaryText returns the secondary text style.
func (s *Styles) SecondaryText() lipgloss.Style { return s.fgSecondary }

// Pinned returns the pinned cell style.
func (s *Styles) Pinned() lipgloss.Style { return s.fgMuted }

// FilterMark returns the filter mark style.
func (s *Styles) FilterMark() lipgloss.Style { return s.fgMuted }

// TableSeparator returns the table separator style.
func (s *Styles) TableSeparator() lipgloss.Style { return s.fgBorder }

// TableHeader returns the table header style.
func (s *Styles) TableHeader() lipgloss.Style { return s.fgTextDimBold }

// Null returns the null value style.
func (s *Styles) Null() lipgloss.Style { return s.fgTextDimItalic }

// ColActiveHeader returns the active column header style.
func (s *Styles) ColActiveHeader() lipgloss.Style { return s.colActiveHeader }

// HiddenLeft returns the hidden-left indicator style.
func (s *Styles) HiddenLeft() lipgloss.Style { return s.fgSecondaryItalic }

// HiddenRight returns the hidden-right indicator style.
func (s *Styles) HiddenRight() lipgloss.Style { return s.fgAccentItalic }

// Info returns the info message style.
func (s *Styles) Info() lipgloss.Style  { return s.fgSuccessBold }
func (s *Styles) Error() lipgloss.Style { return s.fgDangerBold }

// ── Accessors — composite / badge styles ────────────────────────────────────

// Base returns the base style.
func (s *Styles) Base() lipgloss.Style { return s.base }

// Header returns the header (bold) style.
func (s *Styles) Header() lipgloss.Style { return s.bold }

// AccentPill returns the accent pill badge style.
func (s *Styles) AccentPill() lipgloss.Style { return s.accentPill }

// TabActive returns the active tab style.
func (s *Styles) TabActive() lipgloss.Style { return s.accentPill }

// ModeNormal returns the normal-mode badge style.
func (s *Styles) ModeNormal() lipgloss.Style { return s.accentPill }

// HeaderSection returns the header section style.
func (s *Styles) HeaderSection() lipgloss.Style { return s.headerSection }

// TabInactive returns the inactive tab style.
func (s *Styles) TabInactive() lipgloss.Style { return s.tabInactive }

// TabLocked returns the locked tab style.
func (s *Styles) TabLocked() lipgloss.Style { return s.tabLocked }

// TableSelected returns the selected table row style.
func (s *Styles) TableSelected() lipgloss.Style { return s.tableSelected }

// ModeEdit returns the edit-mode badge style.
func (s *Styles) ModeEdit() lipgloss.Style { return s.modeEdit }

// ModeVisual returns the visual-mode badge style.
func (s *Styles) ModeVisual() lipgloss.Style { return s.modeVisualBadge }

// NormalCursor returns the normal-mode cursor style.
func (s *Styles) NormalCursor() lipgloss.Style { return s.normalCursor }

// EditCursor returns the edit-mode cursor style.
func (s *Styles) EditCursor() lipgloss.Style { return s.editCursor }

// VisualSelected returns the visual-mode selected row style.
func (s *Styles) VisualSelected() lipgloss.Style { return s.visualSelected }

// VisualCursor returns the visual-mode cursor style.
func (s *Styles) VisualCursor() lipgloss.Style { return s.visualCursor }

// OverlayBox returns the overlay box style.
func (s *Styles) OverlayBox() lipgloss.Style { return s.overlayBox }

// ── Accessors — help rendering ───────────────────────────────────────────────

// HelpDesc returns the help description style.
func (s *Styles) HelpDesc() lipgloss.Style { return s.helpDesc }

// HelpSection returns the help section heading style.
func (s *Styles) HelpSection() lipgloss.Style { return s.helpSection }

// HelpHint returns the help hint style.
func (s *Styles) HelpHint() lipgloss.Style { return s.helpHint }

// HelpUsage returns the help usage style.
func (s *Styles) HelpUsage() lipgloss.Style { return s.helpUsage }

// ── Shared helpers ──────────────────────────────────────────────────────────

// Keybinds renders a row of pill-shaped keycap + label pairs for footers.
// Pass alternating key, label strings: Keybinds("space", "toggle", "q", "quit")
func (s *Styles) Keybinds(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		key := s.keycap.Render(pairs[i])
		label := s.footer.Render(pairs[i+1])
		parts = append(parts, key+" "+label)
	}
	return strings.Join(parts, " ")
}

// RenderDivider returns a horizontal rule at the given content width.
func (s *Styles) RenderDivider(contentWidth int) string {
	w := FallbackDividerWidth
	if contentWidth > len(DividerLeadingSpaces) {
		w = contentWidth - len(DividerLeadingSpaces)
	}
	return DividerLeadingSpaces + s.divider.Render(strings.Repeat("─", w))
}
