package ui

// styles.go — the single source of truth for all lipgloss styles.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// Styles holds pre-built lipgloss styles shared across all TUI commands.
type Styles struct {
	// palette is retained so callers that need raw colors can access them.
	palette uikit.Palette

	// ── Semantic foreground ──────────────────────────────────────────────

	pass  lipgloss.Style
	fail  lipgloss.Style
	warn  lipgloss.Style
	dim   lipgloss.Style
	dimIt lipgloss.Style
	bold  lipgloss.Style

	// ── Text color (Tailwind-inspired: text{Color}{Modifier}) ───────────

	textDim    lipgloss.Style
	textMid    lipgloss.Style
	textBright lipgloss.Style
	textBlue   lipgloss.Style
	textOrange lipgloss.Style
	textGreen  lipgloss.Style
	textRed    lipgloss.Style
	textPink   lipgloss.Style
	textBorder lipgloss.Style

	// Bold / italic variants
	textDimBold      lipgloss.Style
	textDimItalic    lipgloss.Style
	textBlueBold     lipgloss.Style
	textBlueItalic   lipgloss.Style
	textOrangeBold   lipgloss.Style
	textOrangeItalic lipgloss.Style
	textGreenBold    lipgloss.Style
	textPinkBold     lipgloss.Style
	textRedBold      lipgloss.Style
	colActiveHeader  lipgloss.Style

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
	pillBlue        lipgloss.Style
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
var TUI = NewStyles(uikit.DetectDark())

// NewStyles creates a Styles instance for the given light/dark mode.
func NewStyles(isDark bool) *Styles {
	p := uikit.NewPalette(isDark)
	ld := lipgloss.LightDark(isDark)

	return &Styles{
		palette: p,

		// Semantic foreground
		pass:  lipgloss.NewStyle().Foreground(p.Green),
		fail:  lipgloss.NewStyle().Foreground(p.Red),
		warn:  lipgloss.NewStyle().Foreground(p.Orange),
		dim:   lipgloss.NewStyle().Faint(true),
		dimIt: lipgloss.NewStyle().Faint(true).Italic(true),
		bold:  lipgloss.NewStyle().Bold(true),

		// Text color (Tailwind-inspired: text{Color}{Modifier})
		textDim:    lipgloss.NewStyle().Foreground(p.TextDim),
		textMid:    lipgloss.NewStyle().Foreground(p.TextMid),
		textBright: lipgloss.NewStyle().Foreground(p.TextBright),
		textBlue:   lipgloss.NewStyle().Foreground(p.Blue),
		textOrange: lipgloss.NewStyle().Foreground(p.Orange),
		textGreen:  lipgloss.NewStyle().Foreground(p.Green),
		textRed:    lipgloss.NewStyle().Foreground(p.Red),
		textPink:   lipgloss.NewStyle().Foreground(p.Pink),
		textBorder: lipgloss.NewStyle().Foreground(p.Border),

		// Bold / italic variants
		textDimBold:      lipgloss.NewStyle().Foreground(p.TextDim).Bold(true),
		textDimItalic:    lipgloss.NewStyle().Foreground(p.TextDim).Italic(true),
		textBlueBold:     lipgloss.NewStyle().Foreground(p.Blue).Bold(true),
		textBlueItalic:   lipgloss.NewStyle().Foreground(p.Blue).Italic(true),
		textOrangeBold:   lipgloss.NewStyle().Foreground(p.Orange).Bold(true),
		textOrangeItalic: lipgloss.NewStyle().Foreground(p.Orange).Italic(true),
		textGreenBold:    lipgloss.NewStyle().Foreground(p.Green).Bold(true),
		colActiveHeader: lipgloss.NewStyle().
			Foreground(p.Green).
			Background(ld(lipgloss.Color("#E6F4EA"), lipgloss.Color("#1A3329"))).
			Bold(true),
		textPinkBold: lipgloss.NewStyle().Foreground(p.Pink).Bold(true),
		textRedBold:  lipgloss.NewStyle().Foreground(p.Red).Bold(true),

		// Layout / chrome
		heading: lipgloss.NewStyle().Foreground(p.TextBright).Bold(true),
		cursor:  lipgloss.NewStyle().Background(p.SurfaceRaised).Bold(true),
		title: lipgloss.NewStyle().
			Foreground(p.Blue).
			Bold(true).
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Foreground(p.TextMid).
			Padding(0, 1),
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Pink).
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
		pillBlue: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Blue).
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
			Foreground(p.Green).
			Bold(true),
		editCursor: lipgloss.NewStyle().
			Background(ld(lipgloss.Color("#FFF3E0"), lipgloss.Color("#2D1F0E"))).
			Foreground(p.TextBright).
			Bold(true).
			Underline(true),
		modeEdit: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Orange).
			Padding(0, 1).
			Bold(true),
		modeVisualBadge: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Pink).
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
			BorderForeground(p.Blue).
			Padding(1, 2),

		// Help rendering
		helpDesc: lipgloss.NewStyle().
			Foreground(p.TextMid).
			PaddingLeft(2),
		helpSection: lipgloss.NewStyle().
			Foreground(p.Orange).
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
func (s *Styles) Palette() uikit.Palette { return s.palette }

// ── Accessors — semantic (domain-meaning, color-agnostic) ───────────────────

// Pass returns the pass/success style (green).
func (s *Styles) Pass() lipgloss.Style { return s.pass }

// Fail returns the failure/error style (red).
func (s *Styles) Fail() lipgloss.Style { return s.fail }

// Warn returns the warning style (orange).
func (s *Styles) Warn() lipgloss.Style { return s.warn }

// Dim returns the dim text style.
func (s *Styles) Dim() lipgloss.Style { return s.dim }

// DimIt returns the dim italic style.
func (s *Styles) DimIt() lipgloss.Style { return s.dimIt }

// Bold returns the bold style.
func (s *Styles) Bold() lipgloss.Style { return s.bold }

// ── Accessors — layout / chrome ─────────────────────────────────────────────

// Heading returns the heading style.
func (s *Styles) Heading() lipgloss.Style { return s.heading }

// Cursor returns the cursor style.
func (s *Styles) Cursor() lipgloss.Style { return s.cursor }

// Title returns the title style.
func (s *Styles) Title() lipgloss.Style { return s.title }

// Footer returns the footer style.
func (s *Styles) Footer() lipgloss.Style { return s.footer }

// Panel returns the panel style.
func (s *Styles) Panel() lipgloss.Style { return s.panel }

// Page returns the page style.
func (s *Styles) Page() lipgloss.Style { return s.page }

// Divider returns the divider style.
func (s *Styles) Divider() lipgloss.Style { return s.divider }

// Keycap returns the keycap style.
func (s *Styles) Keycap() lipgloss.Style { return s.keycap }

// ── Accessors — text color (Tailwind-inspired: Text{Color}{Modifier}) ──────

// TextDim returns the dim foreground style.
func (s *Styles) TextDim() lipgloss.Style { return s.textDim }

// TextMid returns the mid-brightness foreground style.
func (s *Styles) TextMid() lipgloss.Style { return s.textMid }

// TextBright returns the bright foreground style.
func (s *Styles) TextBright() lipgloss.Style { return s.textBright }

// TextBlue returns the blue foreground style.
func (s *Styles) TextBlue() lipgloss.Style { return s.textBlue }

// TextOrange returns the orange foreground style.
func (s *Styles) TextOrange() lipgloss.Style { return s.textOrange }

// TextGreen returns the green foreground style.
func (s *Styles) TextGreen() lipgloss.Style { return s.textGreen }

// TextRed returns the red foreground style.
func (s *Styles) TextRed() lipgloss.Style { return s.textRed }

// TextPink returns the pink foreground style.
func (s *Styles) TextPink() lipgloss.Style { return s.textPink }

// TextBorder returns the border foreground style.
func (s *Styles) TextBorder() lipgloss.Style { return s.textBorder }

// ── Accessors — text color bold/italic variants ─────────────────────────────

// TextBlueBold returns the bold blue text style.
func (s *Styles) TextBlueBold() lipgloss.Style { return s.textBlueBold }

// TextBlueItalic returns the italic blue text style.
func (s *Styles) TextBlueItalic() lipgloss.Style { return s.textBlueItalic }

// TextOrangeBold returns the bold orange text style.
func (s *Styles) TextOrangeBold() lipgloss.Style { return s.textOrangeBold }

// TextOrangeItalic returns the italic orange text style.
func (s *Styles) TextOrangeItalic() lipgloss.Style { return s.textOrangeItalic }

// TextGreenBold returns the bold green text style.
func (s *Styles) TextGreenBold() lipgloss.Style { return s.textGreenBold }

// TextPinkBold returns the bold pink text style.
func (s *Styles) TextPinkBold() lipgloss.Style { return s.textPinkBold }

// TextRedBold returns the bold red text style.
func (s *Styles) TextRedBold() lipgloss.Style { return s.textRedBold }

// TextDimBold returns the bold dim text style.
func (s *Styles) TextDimBold() lipgloss.Style { return s.textDimBold }

// TextDimItalic returns the italic dim text style.
func (s *Styles) TextDimItalic() lipgloss.Style { return s.textDimItalic }

// ── Accessors — semantic aliases (db TUI) ───────────────────────────────────

// Readonly returns the read-only cell style.
func (s *Styles) Readonly() lipgloss.Style { return s.textDim }

// Empty returns the empty cell style.
func (s *Styles) Empty() lipgloss.Style { return s.textDim }

// CellDim returns the dim cell style.
func (s *Styles) CellDim() lipgloss.Style { return s.textDim }

// HeaderHint returns the header hint style.
func (s *Styles) HeaderHint() lipgloss.Style { return s.textMid }

// TabUnderline returns the tab underline style.
func (s *Styles) TabUnderline() lipgloss.Style { return s.textBlue }

// SortArrow returns the sort arrow style.
func (s *Styles) SortArrow() lipgloss.Style { return s.textOrange }

// Pinned returns the pinned cell style.
func (s *Styles) Pinned() lipgloss.Style { return s.textPink }

// FilterMark returns the filter mark style.
func (s *Styles) FilterMark() lipgloss.Style { return s.textPink }

// TableSeparator returns the table separator style.
func (s *Styles) TableSeparator() lipgloss.Style { return s.textBorder }

// TableHeader returns the table header style.
func (s *Styles) TableHeader() lipgloss.Style { return s.textDimBold }

// Null returns the null value style.
func (s *Styles) Null() lipgloss.Style { return s.textDimItalic }

// ColActiveHeader returns the active column header style.
func (s *Styles) ColActiveHeader() lipgloss.Style { return s.colActiveHeader }

// HiddenLeft returns the hidden-left indicator style.
func (s *Styles) HiddenLeft() lipgloss.Style { return s.textOrangeItalic }

// HiddenRight returns the hidden-right indicator style.
func (s *Styles) HiddenRight() lipgloss.Style { return s.textBlueItalic }

// Info returns the info message style.
func (s *Styles) Info() lipgloss.Style  { return s.textGreenBold }
func (s *Styles) Error() lipgloss.Style { return s.textRedBold }

// ── Accessors — composite / badge styles ────────────────────────────────────

// Base returns the base style.
func (s *Styles) Base() lipgloss.Style { return s.base }

// Header returns the header (bold) style.
func (s *Styles) Header() lipgloss.Style { return s.bold }

// PillBlue returns the blue pill badge style.
func (s *Styles) PillBlue() lipgloss.Style { return s.pillBlue }

// TabActive returns the active tab style.
func (s *Styles) TabActive() lipgloss.Style { return s.pillBlue }

// ModeNormal returns the normal-mode badge style.
func (s *Styles) ModeNormal() lipgloss.Style { return s.pillBlue }

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
	w := uikit.FallbackDividerWidth
	if contentWidth > len(uikit.DividerLeadingSpaces) {
		w = contentWidth - len(uikit.DividerLeadingSpaces)
	}
	return uikit.DividerLeadingSpaces + s.divider.Render(strings.Repeat("─", w))
}
