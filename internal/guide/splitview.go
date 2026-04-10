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

// splitMinW is the narrowest terminal width that still gets a side-by-side
// layout. Below this we switch to a stacked (top/bottom) arrangement so each
// panel gets the full width.
const splitMinW = 80

// splitChromeLines is the vertical overhead: title (1) + gap (1) + gap (1) + footer (1).
const splitChromeLines = 4

// stackedDividerLines is the vertical overhead of the horizontal divider
// between the two stacked panels: divider (1) + gap below (1).
const stackedDividerLines = 2

// minBodyH is the smallest body height we'll render.
const minBodyH = 3

// stacked returns true when the terminal is too narrow for side-by-side panels.
func (s *splitView) stacked() bool {
	return s.width < splitMinW
}

// SetSize updates layout dimensions and resizes both panels.
func (s *splitView) SetSize(w, h int) {
	s.width = w
	s.height = h

	if w < 1 || h < 1 {
		return // View() will return "" for degenerate sizes.
	}

	bodyH := h - splitChromeLines
	if bodyH < minBodyH {
		bodyH = minBodyH
	}

	if s.stacked() {
		// Stacked: markdown on top, cast on bottom, full width each.
		// Split body height 50/50, minus the horizontal divider.
		availH := bodyH - stackedDividerLines
		if availH < 2 {
			availH = 2
		}
		topH := availH / 2
		botH := availH - topH

		s.viewer.SetSize(w, topH)
		s.player.SetHeight(botH)
		s.player.SetWidth(w)
	} else {
		// Side-by-side: markdown left, cast right.
		leftW, rightW := splitPanelWidths(w, s.player.cast.Header.Width)
		s.viewer.SetSize(leftW, bodyH)
		s.player.SetHeight(bodyH)
		s.player.SetWidth(rightW)
	}
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

// View renders the side-by-side or stacked layout depending on terminal width.
func (s *splitView) View() string {
	if s.width < 1 || s.height < 1 {
		return ""
	}

	var b strings.Builder

	// Title bar
	b.WriteString(ui.TUI.HeaderSection().Render(" " + s.title + " "))
	b.WriteString("\n\n")

	if s.stacked() {
		b.WriteString(s.viewStacked())
	} else {
		b.WriteString(s.viewSideBySide())
	}

	b.WriteString("\n")

	// Footer
	b.WriteString(s.footer())

	return b.String()
}

// viewSideBySide renders panels left-to-right with a vertical divider.
func (s *splitView) viewSideBySide() string {
	leftW, rightW := splitPanelWidths(s.width, s.player.cast.Header.Width)

	bodyH := s.height - splitChromeLines
	if bodyH < minBodyH {
		bodyH = minBodyH
	}

	leftBox := lipgloss.NewStyle().
		Width(leftW).
		Height(bodyH).
		PaddingRight(1).
		Render(s.viewer.View())

	rightBox := lipgloss.NewStyle().
		Width(rightW).
		Height(bodyH).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeftForeground(ui.TUI.Palette().Accent).
		PaddingLeft(1).
		Render(s.player.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}

// viewStacked renders markdown on top and the cast player below with a
// horizontal accent divider.
func (s *splitView) viewStacked() string {
	bodyH := s.height - splitChromeLines
	if bodyH < minBodyH {
		bodyH = minBodyH
	}
	availH := bodyH - stackedDividerLines
	if availH < 2 {
		availH = 2
	}
	topH := availH / 2
	botH := availH - topH

	topBox := lipgloss.NewStyle().
		Width(s.width).
		Height(topH).
		Render(s.viewer.View())

	divider := lipgloss.NewStyle().
		Foreground(ui.TUI.Palette().Accent).
		Render(strings.Repeat("━", s.width))

	botBox := lipgloss.NewStyle().
		Width(s.width).
		Height(botH).
		Render(s.player.View())

	return topBox + "\n" + divider + "\n" + botBox
}

// footer renders a combined hint line for both panels, progressively dropping
// less-important hints as the terminal narrows.
func (s *splitView) footer() string {
	if s.viewer.Searching() {
		return "" // search input is visible in the viewer
	}

	const sep = "  "

	// Hints ordered most → least important. We always keep esc and scroll %.
	// The rest are dropped right-to-left as width shrinks.
	type hint struct {
		text string
	}
	hints := []hint{
		{"↑/↓ scroll"},
		{"/ search"},
		{"space pause/play"},
		{"r restart"},
	}
	if s.viewer.Query() != "" {
		hints = append(hints, hint{
			fmt.Sprintf("n/N next/prev (%d matches)", s.viewer.MatchCount()),
		})
	}

	// Always-present suffix: esc + scroll percent.
	escPart := ui.TUI.HeaderHint().Render("esc close")
	pctPart := ui.TUI.Dim().Render(fmt.Sprintf("%d%%", s.viewer.ScrollPercent()))
	suffix := escPart + sep + pctPart
	suffixW := lipgloss.Width(suffix)

	// Build from left, dropping hints that don't fit.
	budget := s.width - suffixW
	var parts []string
	for _, h := range hints {
		rendered := ui.TUI.HeaderHint().Render(h.text)
		w := lipgloss.Width(rendered) + len(sep)
		if budget-w < 0 {
			break // remaining hints won't fit
		}
		parts = append(parts, rendered)
		budget -= w
	}

	parts = append(parts, suffix)
	return strings.Join(parts, sep)
}
