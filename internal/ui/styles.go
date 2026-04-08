package ui

// styles.go — the single source of truth for all lipgloss styles. Command-
// specific aliases are fine, but new visual styles belong here.

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// Styles holds pre-built lipgloss styles shared across all TUI commands.
// This is the single source of truth — command-specific aliases are fine
// but new styles belong here.
type Styles struct {
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

	// Install-method badges (doc TUI tool list)
	badgeBrew  lipgloss.Style
	badgeUV    lipgloss.Style
	badgeGo    lipgloss.Style
	badgeNPM   lipgloss.Style
	badgeCargo lipgloss.Style
	badgeOther lipgloss.Style

	// Help rendering
	helpDesc    lipgloss.Style
	helpSection lipgloss.Style
	helpHint    lipgloss.Style
	helpUsage   lipgloss.Style

	// palette is retained so callers that need raw colors can access them.
	palette Palette
}

// TUI is the package-level shared styles singleton.
var TUI = NewStyles(DetectDark())

// Palette returns the resolved color palette backing this Styles instance.
func (s *Styles) Palette() Palette { return s.palette }

// NewStyles creates a Styles instance for the given light/dark mode.
func NewStyles(isDark bool) *Styles {
	p := NewPalette(isDark)
	ld := lipgloss.LightDark(isDark)

	return &Styles{
		palette: p,

		// Semantic foreground (doc TUI originals)
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
		fgMutedBold:       lipgloss.NewStyle().Foreground(p.Muted).Bold(true),
		fgDangerBold:      lipgloss.NewStyle().Foreground(p.Danger).Bold(true),

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
			Background(ld(lipgloss.Color("#E0EEF9"), lipgloss.Color("#1A2744"))).
			Foreground(p.TextBright).
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

		// Install-method badges
		badgeBrew: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Secondary).
			Padding(0, 1),
		badgeUV: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Success).
			Padding(0, 1),
		badgeGo: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Accent).
			Padding(0, 1),
		badgeNPM: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Danger).
			Padding(0, 1),
		badgeCargo: lipgloss.NewStyle().
			Foreground(p.OnAccent).
			Background(p.Muted).
			Padding(0, 1),
		badgeOther: lipgloss.NewStyle().
			Foreground(p.TextDim).
			Padding(0, 1),

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

// ── Accessors — doc TUI originals ────────────────────────────────────────

func (s *Styles) Pass() lipgloss.Style    { return s.pass }
func (s *Styles) Fail() lipgloss.Style    { return s.fail }
func (s *Styles) Warn() lipgloss.Style    { return s.warn }
func (s *Styles) Accent() lipgloss.Style  { return s.accent }
func (s *Styles) Muted() lipgloss.Style   { return s.muted }
func (s *Styles) Dim() lipgloss.Style     { return s.dim }
func (s *Styles) DimIt() lipgloss.Style   { return s.dimIt }
func (s *Styles) Bold() lipgloss.Style    { return s.bold }
func (s *Styles) Heading() lipgloss.Style { return s.heading }
func (s *Styles) Cursor() lipgloss.Style  { return s.cursor }
func (s *Styles) Title() lipgloss.Style   { return s.title }
func (s *Styles) Footer() lipgloss.Style  { return s.footer }
func (s *Styles) Panel() lipgloss.Style   { return s.panel }
func (s *Styles) Page() lipgloss.Style    { return s.page }
func (s *Styles) Divider() lipgloss.Style { return s.divider }
func (s *Styles) Keycap() lipgloss.Style  { return s.keycap }

// ── Accessors — fine-grained foreground ─────────────────────────────────────

func (s *Styles) TextDim() lipgloss.Style     { return s.fgTextDim }
func (s *Styles) TextMid() lipgloss.Style     { return s.fgTextMid }
func (s *Styles) TextBright() lipgloss.Style  { return s.fgTextBright }
func (s *Styles) FgAccent() lipgloss.Style    { return s.fgAccent }
func (s *Styles) FgSecondary() lipgloss.Style { return s.fgSecondary }
func (s *Styles) FgSuccess() lipgloss.Style   { return s.fgSuccess }
func (s *Styles) FgDanger() lipgloss.Style    { return s.fgDanger }
func (s *Styles) FgMuted() lipgloss.Style     { return s.fgMuted }
func (s *Styles) FgBorder() lipgloss.Style    { return s.fgBorder }

// ── Accessors — bold/italic variants ────────────────────────────────────────

func (s *Styles) AccentBold() lipgloss.Style      { return s.fgAccentBold }
func (s *Styles) AccentItalic() lipgloss.Style    { return s.fgAccentItalic }
func (s *Styles) SecondaryBold() lipgloss.Style   { return s.fgSecondaryBold }
func (s *Styles) SecondaryItalic() lipgloss.Style { return s.fgSecondaryItalic }
func (s *Styles) SuccessBold() lipgloss.Style     { return s.fgSuccessBold }
func (s *Styles) MutedBold() lipgloss.Style       { return s.fgMutedBold }
func (s *Styles) DangerBold() lipgloss.Style      { return s.fgDangerBold }
func (s *Styles) TextDimBold() lipgloss.Style     { return s.fgTextDimBold }
func (s *Styles) TextDimItalic() lipgloss.Style   { return s.fgTextDimItalic }

// ── Accessors — semantic aliases (db TUI) ───────────────────────────────────
// These return the same underlying style under a domain-specific name.

func (s *Styles) Readonly() lipgloss.Style        { return s.fgTextDim }
func (s *Styles) Empty() lipgloss.Style           { return s.fgTextDim }
func (s *Styles) CellDim() lipgloss.Style         { return s.fgTextDim }
func (s *Styles) HeaderHint() lipgloss.Style      { return s.fgTextMid }
func (s *Styles) TabUnderline() lipgloss.Style    { return s.fgAccent }
func (s *Styles) AccentText() lipgloss.Style      { return s.fgAccent }
func (s *Styles) SortArrow() lipgloss.Style       { return s.fgSecondary }
func (s *Styles) SecondaryText() lipgloss.Style   { return s.fgSecondary }
func (s *Styles) Pinned() lipgloss.Style          { return s.fgMuted }
func (s *Styles) FilterMark() lipgloss.Style      { return s.fgMuted }
func (s *Styles) TableSeparator() lipgloss.Style  { return s.fgBorder }
func (s *Styles) TableHeader() lipgloss.Style     { return s.fgTextDimBold }
func (s *Styles) Null() lipgloss.Style            { return s.fgTextDimItalic }
func (s *Styles) ColActiveHeader() lipgloss.Style { return s.fgSuccessBold }
func (s *Styles) HiddenLeft() lipgloss.Style      { return s.fgSecondaryItalic }
func (s *Styles) HiddenRight() lipgloss.Style     { return s.fgAccentItalic }
func (s *Styles) Info() lipgloss.Style            { return s.fgSuccessBold }
func (s *Styles) Error() lipgloss.Style           { return s.fgDangerBold }

// ── Accessors — composite / badge styles ────────────────────────────────────

func (s *Styles) Base() lipgloss.Style           { return s.base }
func (s *Styles) Header() lipgloss.Style         { return s.bold }
func (s *Styles) AccentPill() lipgloss.Style     { return s.accentPill }
func (s *Styles) TabActive() lipgloss.Style      { return s.accentPill }
func (s *Styles) ModeNormal() lipgloss.Style     { return s.accentPill }
func (s *Styles) HeaderSection() lipgloss.Style  { return s.headerSection }
func (s *Styles) TabInactive() lipgloss.Style    { return s.tabInactive }
func (s *Styles) TabLocked() lipgloss.Style      { return s.tabLocked }
func (s *Styles) TableSelected() lipgloss.Style  { return s.tableSelected }
func (s *Styles) ModeEdit() lipgloss.Style       { return s.modeEdit }
func (s *Styles) ModeVisual() lipgloss.Style     { return s.modeVisualBadge }
func (s *Styles) NormalCursor() lipgloss.Style   { return s.normalCursor }
func (s *Styles) EditCursor() lipgloss.Style     { return s.editCursor }
func (s *Styles) VisualSelected() lipgloss.Style { return s.visualSelected }
func (s *Styles) VisualCursor() lipgloss.Style   { return s.visualCursor }
func (s *Styles) OverlayBox() lipgloss.Style     { return s.overlayBox }

// ── Accessors — install-method badges ───────────────────────────────────────

func (s *Styles) BadgeBrew() lipgloss.Style  { return s.badgeBrew }
func (s *Styles) BadgeUV() lipgloss.Style    { return s.badgeUV }
func (s *Styles) BadgeGo() lipgloss.Style    { return s.badgeGo }
func (s *Styles) BadgeNPM() lipgloss.Style   { return s.badgeNPM }
func (s *Styles) BadgeCargo() lipgloss.Style { return s.badgeCargo }
func (s *Styles) BadgeOther() lipgloss.Style { return s.badgeOther }

// ── Accessors — help rendering ───────────────────────────────────────────────

func (s *Styles) HelpDesc() lipgloss.Style    { return s.helpDesc }
func (s *Styles) HelpSection() lipgloss.Style { return s.helpSection }
func (s *Styles) HelpHint() lipgloss.Style    { return s.helpHint }
func (s *Styles) HelpUsage() lipgloss.Style   { return s.helpUsage }

// NewListDelegate returns a list.DefaultDelegate styled to match the TUI theme.
// Used by the help browser and glossary TUIs.
func NewListDelegate() list.DefaultDelegate {
	p := TUI.palette
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(p.Accent).
		BorderLeftForeground(p.Accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(p.Accent).
		BorderLeftForeground(p.Accent)
	return d
}

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
// Pass ContentWidth (not raw terminal width) so the rule fits inside
// PageLayout's padding.
func (s *Styles) RenderDivider(contentWidth int) string {
	w := FallbackDividerWidth
	if contentWidth > len(DividerLeadingSpaces) {
		w = contentWidth - len(DividerLeadingSpaces)
	}
	return DividerLeadingSpaces + s.divider.Render(strings.Repeat("─", w))
}
