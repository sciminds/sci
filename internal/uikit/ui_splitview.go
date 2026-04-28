package uikit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ScrollPanel is a side-by-side-ready sub-model. Each panel owns its own
// scrollable content, handles its own messages, and renders its own footer.
// SplitView composes two panels into a responsive layout.
//
//   - Implementations: MdViewer (default reader), CastPlayer (specialist).
//   - Wider panels (no preferred width) occupy leftover space; specialists
//     with a PreferredWidth > 0 get up to that width capped at 45% of usable.
type ScrollPanel interface {
	Init() tea.Cmd
	View() string
	SetSize(w, h int)
	Handle(msg tea.Msg) tea.Cmd
	// Footer renders the hint line shown directly below the panel,
	// trimming hints to fit the given width.
	Footer(width int) string
	// CapturesInput returns true when the panel is in a mode that should
	// receive every key (e.g. active search input). SplitView routes all
	// keys to the capturing panel.
	CapturesInput() bool
	// OwnsKey returns true when the panel claims this specific key even
	// when its sibling is the default recipient (e.g. CastPlayer claims
	// "space" and "r").
	OwnsKey(key string) bool
	// PreferredWidth returns the panel's desired width in side-by-side
	// mode. 0 means "no preference" — panel takes leftover space.
	PreferredWidth() int
}

// SplitView composes two ScrollPanels into a responsive layout: side-by-side
// with a vertical accent divider on wide terminals, stacked top/bottom on
// narrow ones. The left panel is the default recipient for unclaimed keys;
// the right panel is the specialist that claims specific keys via OwnsKey.
type SplitView struct {
	title  string
	left   ScrollPanel
	right  ScrollPanel
	width  int
	height int
}

// NewSplitView creates a split view titled with the given string. Left is
// the default-recipient panel (typically a reader / MdViewer); right is the
// specialist panel (typically a CastPlayer or similar).
func NewSplitView(title string, left, right ScrollPanel) *SplitView {
	return &SplitView{title: title, left: left, right: right}
}

// ── layout constants ──────────────────────────────────────────────────────

const (
	splitDividerCols    = 3  // vertical divider: border(1) + paddingL(1) + paddingR(1)
	splitMinPanelW      = 20 // narrowest allowable panel width
	splitMinW           = 80 // below this, switch to stacked layout
	splitChromeLines    = 2  // title (1) + gap (1)
	splitFooterLines    = 1  // per-panel footer row
	splitStackedDivider = 2  // horizontal divider (1) + gap (1)
	splitMinBodyH       = 3
)

// stacked reports whether the current width forces a stacked layout.
func (s *SplitView) stacked() bool { return s.width < splitMinW }

// splitPanelWidths apportions horizontal space between the default panel
// (left) and the specialist (right). The specialist gets its preferred
// width capped at 45% of usable space; the default takes the rest.
func splitPanelWidths(totalW, preferredRight int) (left, right int) {
	usable := totalW - splitDividerCols
	if usable < 2 {
		return 1, 1
	}
	if usable < splitMinPanelW*2 {
		left = usable / 2
		right = usable - left
		return
	}
	right = preferredRight
	maxRight := usable * 45 / 100
	if right > maxRight {
		right = maxRight
	}
	if right < splitMinPanelW {
		right = splitMinPanelW
	}
	left = usable - right
	return
}

// SetSize updates layout dimensions and resizes both panels.
func (s *SplitView) SetSize(w, h int) {
	s.width = w
	s.height = h

	if w < 1 || h < 1 {
		return
	}

	bodyH := h - splitChromeLines
	if bodyH < splitMinBodyH {
		bodyH = splitMinBodyH
	}

	if s.stacked() {
		availH := bodyH - splitStackedDivider
		if availH < 2 {
			availH = 2
		}
		topH := availH / 2
		botH := availH - topH
		s.left.SetSize(w, topH-splitFooterLines)
		s.right.SetSize(w, botH-splitFooterLines)
		return
	}

	leftW, rightW := splitPanelWidths(w, s.right.PreferredWidth())
	panelH := bodyH - splitFooterLines
	s.left.SetSize(leftW, panelH)
	s.right.SetSize(rightW, panelH)
}

// Init starts both panels.
func (s *SplitView) Init() tea.Cmd {
	return tea.Batch(s.left.Init(), s.right.Init())
}

// Update routes messages. Non-key messages go to both panels (each ignores
// what it doesn't recognize). Keys follow: capturing panel first, then
// right.OwnsKey, then left as default.
func (s *SplitView) Update(msg tea.Msg) (*SplitView, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		key := km.String()
		switch {
		case s.left.CapturesInput():
			return s, s.left.Handle(msg)
		case s.right.CapturesInput():
			return s, s.right.Handle(msg)
		case s.right.OwnsKey(key):
			return s, s.right.Handle(msg)
		case s.left.OwnsKey(key):
			return s, s.left.Handle(msg)
		default:
			return s, s.left.Handle(msg)
		}
	}

	return s, tea.Batch(s.left.Handle(msg), s.right.Handle(msg))
}

// Searching reports whether either panel is capturing input.
func (s *SplitView) Searching() bool {
	return s.left.CapturesInput() || s.right.CapturesInput()
}

// View renders the full split view.
func (s *SplitView) View() string {
	if s.width < 1 || s.height < 1 {
		return ""
	}

	var b strings.Builder
	b.WriteString(TUI.HeaderSection().Render(" " + s.title + " "))
	b.WriteString("\n\n")

	if s.stacked() {
		b.WriteString(s.viewStacked())
	} else {
		b.WriteString(s.viewSideBySide())
	}
	return b.String()
}

func (s *SplitView) viewSideBySide() string {
	leftW, rightW := splitPanelWidths(s.width, s.right.PreferredWidth())

	bodyH := s.height - splitChromeLines
	if bodyH < splitMinBodyH {
		bodyH = splitMinBodyH
	}

	// Both panels go through Box so frame size (border + padding) is
	// derived automatically. leftW/rightW are content widths; Box adds the
	// chrome on the outside so the panel's allocated content area matches
	// the (leftW, panelH) / (rightW, panelH) values passed in SetSize.
	leftStyle := TUI.Base().PaddingRight(1)
	leftBox := Box(leftW+leftStyle.GetHorizontalFrameSize(), bodyH, leftStyle,
		func(iw, _ int) string {
			return s.left.View() + "\n" + s.left.Footer(iw)
		})

	rightStyle := TUI.Base().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeftForeground(TUI.Palette().Blue).
		PaddingLeft(1)
	rightBox := Box(rightW+rightStyle.GetHorizontalFrameSize(), bodyH, rightStyle,
		func(iw, _ int) string {
			return s.right.View() + "\n" + s.right.Footer(iw)
		})

	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}

func (s *SplitView) viewStacked() string {
	bodyH := s.height - splitChromeLines
	if bodyH < splitMinBodyH {
		bodyH = splitMinBodyH
	}
	availH := bodyH - splitStackedDivider
	if availH < 2 {
		availH = 2
	}
	topH := availH / 2
	botH := availH - topH

	topContent := s.left.View() + "\n" + s.left.Footer(s.width)
	topBox := TUI.Base().Width(s.width).Height(topH).Render(topContent)

	divider := TUI.TextBlue().Render(strings.Repeat("━", s.width))

	botContent := s.right.View() + "\n" + s.right.Footer(s.width)
	botBox := TUI.Base().Width(s.width).Height(botH).Render(botContent)

	return topBox + "\n" + divider + "\n" + botBox
}
