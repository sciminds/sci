package uikit

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// mdFmtPct formats a scroll percentage for the footer.
func mdFmtPct(pct int) string { return fmt.Sprintf("%d%%", pct) }

// mdFmtMatches formats the match-count hint.
func mdFmtMatches(n int) string { return fmt.Sprintf("n/N next/prev (%d matches)", n) }

// MdViewer is a scrollable, searchable markdown viewer sub-model. Embed in
// other bubbletea programs to show a rendered markdown document with live
// `/` search, match navigation, and scroll percent.
type MdViewer struct {
	vp            viewport.Model
	content       string // raw markdown
	rendered      string // cached glamour output
	renderedWidth int    // width used for cached render
	name          string
	ready         bool
	search        mdSearchState
}

// NewMdViewer creates a viewer for a single markdown document.
func NewMdViewer(name, markdown string) *MdViewer {
	return &MdViewer{
		name:    name,
		content: markdown,
		search:  newMdSearchState(),
	}
}

// SetSize configures the viewport dimensions and re-renders content.
func (v *MdViewer) SetSize(w, h int) {
	contentW := w - 2
	if contentW < 20 {
		contentW = 20
	}

	if v.rendered == "" || v.renderedWidth != contentW {
		rendered, err := RenderMarkdown(v.content, contentW)
		if err != nil {
			rendered = v.content
		}
		v.rendered = strings.TrimRight(rendered, "\n")
		v.renderedWidth = contentW
	}

	if !v.ready {
		v.vp = viewport.New(
			viewport.WithWidth(w),
			viewport.WithHeight(h),
		)
		v.vp.SoftWrap = true
		v.ready = true
	} else {
		v.vp.SetWidth(w)
		v.vp.SetHeight(h)
	}
	v.vp.SetContent(v.rendered)
}

// Searching returns true when the search input is active.
func (v *MdViewer) Searching() bool { return v.search.searching }

// MatchCount returns the number of search matches.
func (v *MdViewer) MatchCount() int { return v.search.matchCount }

// Query returns the current search query.
func (v *MdViewer) Query() string { return v.search.query }

// RawContent returns the original markdown source.
func (v *MdViewer) RawContent() string { return v.content }

// ScrollPercent returns the current scroll position as 0-100.
func (v *MdViewer) ScrollPercent() int {
	if !v.ready {
		return 0
	}
	return mdScrollPercent(&v.vp)
}

// Update handles key and scroll messages.
func (v *MdViewer) Update(msg tea.Msg) (*MdViewer, tea.Cmd) {
	if !v.ready {
		return v, nil
	}

	if v.search.searching {
		return v.updateSearch(msg)
	}

	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "/", "f":
			return v, v.search.focus()
		case "n":
			v.search.nextMatch(&v.vp)
			return v, nil
		case "N":
			v.search.prevMatch(&v.vp)
			return v, nil
		}
	}

	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return v, cmd
}

func (v *MdViewer) updateSearch(msg tea.Msg) (*MdViewer, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter":
			v.search.confirm(&v.vp, v.rendered)
			return v, nil
		case "esc":
			v.search.clear(&v.vp, v.rendered)
			return v, nil
		}
	}

	return v, v.search.liveUpdate(msg, &v.vp, v.rendered)
}

// Init implements ScrollPanel. MdViewer has no startup command.
func (v *MdViewer) Init() tea.Cmd { return nil }

// Handle implements ScrollPanel.
func (v *MdViewer) Handle(msg tea.Msg) tea.Cmd {
	_, cmd := v.Update(msg)
	return cmd
}

// Footer implements ScrollPanel. Renders scroll/search hints + scroll percent,
// trimming hints to fit the given width.
func (v *MdViewer) Footer(width int) string {
	if v.search.searching {
		return "" // search input is visible in the viewer
	}
	const sep = "  "

	pctPart := TUI.Dim().Render(mdFmtPct(v.ScrollPercent()))
	pctW := lipgloss.Width(pctPart)

	hints := []string{"↑/↓ scroll", "/ search"}
	if v.search.query != "" {
		hints = append(hints, mdFmtMatches(v.search.matchCount))
	}

	budget := width - pctW
	var parts []string
	for _, h := range hints {
		rendered := TUI.HeaderHint().Render(h)
		w := lipgloss.Width(rendered) + len(sep)
		if budget-w < 0 {
			break
		}
		parts = append(parts, rendered)
		budget -= w
	}
	parts = append(parts, pctPart)
	return strings.Join(parts, sep)
}

// CapturesInput implements ScrollPanel: true while the search input is active.
func (v *MdViewer) CapturesInput() bool { return v.search.searching }

// OwnsKey implements ScrollPanel: MdViewer is the default recipient and
// claims no specific keys — its sibling's OwnsKey wins, otherwise routing
// defaults here.
func (v *MdViewer) OwnsKey(string) bool { return false }

// PreferredWidth implements ScrollPanel: 0 means "take leftover space".
func (v *MdViewer) PreferredWidth() int { return 0 }

// View renders the viewport content, with search input appended when active.
func (v *MdViewer) View() string {
	if !v.ready {
		return ""
	}
	if v.search.searching {
		return v.vp.View() + "\n" + v.search.input.View()
	}
	return v.vp.View()
}

// ── search state helpers (unexported) ──────────────────────────────────────

type mdSearchState struct {
	searching  bool
	input      textinput.Model
	query      string
	matchCount int
	matchLines []int
	matchIdx   int
}

func newMdSearchState() mdSearchState {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	return mdSearchState{input: ti}
}

func (s *mdSearchState) clear(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = ""
	s.matchCount = 0
	s.matchLines = nil
	s.matchIdx = 0
	vp.SetContent(rendered)
}

func (s *mdSearchState) confirm(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = s.input.Value()
	s.matchLines, s.matchCount, s.matchIdx = mdApplySearch(s.query, rendered, vp)
}

func (s *mdSearchState) focus() tea.Cmd {
	s.searching = true
	s.input.SetValue("")
	return s.input.Focus()
}

func (s *mdSearchState) liveUpdate(msg tea.Msg, vp *viewport.Model, rendered string) tea.Cmd {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if q := s.input.Value(); q != s.query {
		s.query = q
		s.matchLines, s.matchCount, s.matchIdx = mdApplySearch(s.query, rendered, vp)
	}
	return cmd
}

func (s *mdSearchState) nextMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx + 1) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

func (s *mdSearchState) prevMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx - 1 + len(s.matchLines)) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

func mdScrollPercent(vp *viewport.Model) int {
	total := vp.TotalLineCount()
	visible := vp.VisibleLineCount()
	if total <= visible {
		return 100
	}
	pct := (vp.YOffset() + visible) * 100 / total
	if pct > 100 {
		pct = 100
	}
	return pct
}

// mdApplySearch finds all case-insensitive matches of query in rendered,
// updates the viewport content with highlights, and returns the match state.
func mdApplySearch(query, rendered string, vp *viewport.Model) (matchLines []int, matchCount, matchIdx int) {
	if query == "" {
		vp.SetContent(rendered)
		return nil, 0, 0
	}

	plain := ansi.Strip(rendered)
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(query)

	start := 0
	for {
		idx := strings.Index(lowerPlain[start:], lowerQuery)
		if idx < 0 {
			break
		}
		begin := start + idx
		line := strings.Count(lowerPlain[:begin], "\n")
		matchLines = append(matchLines, line)
		start = begin + len(query)
	}

	matchCount = len(matchLines)
	if matchCount > 0 {
		vp.SetContent(HighlightMatches(rendered, query))
		vp.SetYOffset(matchLines[0])
	} else {
		vp.SetContent(rendered)
	}
	return matchLines, matchCount, 0
}
