package guide

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/mdview"
	"github.com/sciminds/cli/internal/ui"
)

// splitView renders a side-by-side layout: scrollable markdown on the left,
// cast player on the right.
type splitView struct {
	viewer *mdview.Viewer
	player *Player
	title  string
	width  int
	height int
}

// newSplitView creates a split view with the given markdown viewer and player.
func newSplitView(title string, viewer *mdview.Viewer, player *Player) *splitView {
	return &splitView{
		title:  title,
		viewer: viewer,
		player: player,
	}
}

// splitPanelWidths computes the left (markdown) and right (cast) panel widths
// for the given total width. The cast panel tries to honour the recording's
// original width, with the markdown panel taking whatever remains.
// dividerCols is the horizontal space consumed by the accent border between
// panels: 1 col border + 1 col padding on each side = 3.
const dividerCols = 3

// minPanelW is the narrowest a single panel is allowed to be.
const minPanelW = 20

func splitPanelWidths(totalW, castW int) (left, right int) {
	usable := totalW - dividerCols
	if usable < 2 {
		// Absurdly narrow — give each panel at least 1 column.
		return 1, 1
	}

	// Below the minimum for both panels, split evenly.
	if usable < minPanelW*2 {
		left = usable / 2
		right = usable - left
		return
	}

	// Give the cast panel its recorded width (capped at 45% of usable).
	right = castW
	maxRight := usable * 45 / 100
	if right > maxRight {
		right = maxRight
	}
	if right < minPanelW {
		right = minPanelW
	}
	left = usable - right
	return
}

// splitChromeLines is the vertical overhead: title (1) + gap (1) + gap (1) + footer (1).
const splitChromeLines = 4

// minBodyH is the smallest body height we'll render.
const minBodyH = 3

// SetSize updates layout dimensions and resizes both panels.
func (s *splitView) SetSize(w, h int) {
	s.width = w
	s.height = h

	if w < 1 || h < 1 {
		return // View() will return "" for degenerate sizes.
	}

	leftW, _ := splitPanelWidths(w, s.player.cast.Header.Width)

	bodyH := h - splitChromeLines
	if bodyH < minBodyH {
		bodyH = minBodyH
	}

	s.viewer.SetSize(leftW, bodyH)
	s.player.SetHeight(bodyH)
}

// Init starts cast playback.
func (s *splitView) Init() tea.Cmd {
	return s.player.Init()
}

// Update routes messages: scroll/search → viewer, space/r → player, ticks → player.
func (s *splitView) Update(msg tea.Msg) (*splitView, tea.Cmd) {
	switch msg := msg.(type) {
	case TickMsg:
		var cmd tea.Cmd
		s.player, cmd = s.player.Update(msg)
		return s, cmd

	case tea.KeyPressMsg:
		// While searching, everything goes to the viewer (except ctrl+c handled by parent).
		if s.viewer.Searching() {
			var cmd tea.Cmd
			s.viewer, cmd = s.viewer.Update(msg)
			return s, cmd
		}

		// Player controls.
		switch msg.String() {
		case "space":
			var cmd tea.Cmd
			s.player, cmd = s.player.Update(msg)
			return s, cmd
		case "r":
			var cmd tea.Cmd
			s.player, cmd = s.player.Update(msg)
			return s, cmd
		}

		// Everything else goes to the viewer (scroll, search, n/N).
		var cmd tea.Cmd
		s.viewer, cmd = s.viewer.Update(msg)
		return s, cmd
	}

	return s, nil
}

// Searching returns true when the viewer's search input is active.
func (s *splitView) Searching() bool {
	return s.viewer.Searching()
}

// View renders the side-by-side layout.
func (s *splitView) View() string {
	if s.width < 1 || s.height < 1 {
		return ""
	}

	leftW, rightW := splitPanelWidths(s.width, s.player.cast.Header.Width)

	bodyH := s.height - splitChromeLines
	if bodyH < minBodyH {
		bodyH = minBodyH
	}

	// ── Left panel (markdown) ──────────────────────────────────────────
	leftContent := s.viewer.View()

	leftBox := lipgloss.NewStyle().
		Width(leftW).
		Height(bodyH).
		PaddingRight(1).
		Render(leftContent)

	// ── Right panel (cast player) ──────────────────────────────────────
	// A left-only border in the accent color acts as a visible divider.
	rightContent := s.player.View()

	rightBox := lipgloss.NewStyle().
		Width(rightW).
		Height(bodyH).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeftForeground(ui.TUI.Palette().Accent).
		PaddingLeft(1).
		Render(rightContent)

	// ── Compose ────────────────────────────────────────────────────────
	var b strings.Builder

	// Title bar
	b.WriteString(ui.TUI.HeaderSection().Render(" " + s.title + " "))
	b.WriteString("\n\n")

	joined := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
	b.WriteString(joined)
	b.WriteString("\n")

	// Footer
	b.WriteString(s.footer())

	return b.String()
}

// footer renders a combined hint line for both panels.
func (s *splitView) footer() string {
	if s.viewer.Searching() {
		return "" // search input is visible in the viewer
	}

	var parts []string
	parts = append(parts, ui.TUI.HeaderHint().Render("↑/↓ scroll"))
	parts = append(parts, ui.TUI.HeaderHint().Render("/ search"))
	parts = append(parts, ui.TUI.HeaderHint().Render("space pause/play"))
	parts = append(parts, ui.TUI.HeaderHint().Render("r restart"))

	if s.viewer.Query() != "" {
		parts = append(parts, ui.TUI.HeaderHint().Render(
			fmt.Sprintf("n/N next/prev (%d matches)", s.viewer.MatchCount()),
		))
	}

	parts = append(parts, ui.TUI.HeaderHint().Render("esc close"))

	// Scroll percent
	pct := fmt.Sprintf("%d%%", s.viewer.ScrollPercent())
	parts = append(parts, ui.TUI.Dim().Render(pct))

	return strings.Join(parts, "  ")
}
