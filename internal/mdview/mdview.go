// Package mdview provides a markdown rendering TUI with file picking, viewport
// scrolling, and live search highlighting. The Viewer sub-model is embeddable
// in other bubbletea programs.
package mdview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sciminds/cli/internal/ui"
)

// Page holds a single markdown document's metadata and raw content.
type Page struct {
	Name    string // display name
	Content string // raw markdown
}

// Title implements list.DefaultItem.
func (p Page) Title() string { return p.Name }

// Description implements list.DefaultItem.
func (p Page) Description() string { return "" }

// FilterValue implements list.DefaultItem.
func (p Page) FilterValue() string { return p.Name }

// level tracks navigation state.
type level int

const (
	levelPicker level = iota // file picker (multi-file mode)
	levelViewer              // rendered markdown viewport
)

// searchState holds the live-search state shared by Model and Viewer.
type searchState struct {
	searching  bool
	input      textinput.Model
	query      string
	matchCount int
	matchLines []int
	matchIdx   int
}

func newSearchState() searchState {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	return searchState{input: ti}
}

// clear exits search mode, resets matches, and restores the viewport content.
func (s *searchState) clear(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = ""
	s.matchCount = 0
	s.matchLines = nil
	s.matchIdx = 0
	vp.SetContent(rendered)
}

// confirm exits search mode and applies the current input as the query.
func (s *searchState) confirm(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = s.input.Value()
	s.matchLines, s.matchCount, s.matchIdx = applySearch(s.query, rendered, vp)
}

// focus enters search mode, clearing the input.
func (s *searchState) focus() tea.Cmd {
	s.searching = true
	s.input.SetValue("")
	return s.input.Focus()
}

// liveUpdate updates the search input and re-highlights on change.
func (s *searchState) liveUpdate(msg tea.Msg, vp *viewport.Model, rendered string) tea.Cmd {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if q := s.input.Value(); q != s.query {
		s.query = q
		s.matchLines, s.matchCount, s.matchIdx = applySearch(s.query, rendered, vp)
	}
	return cmd
}

// nextMatch cycles to the next match and scrolls the viewport.
func (s *searchState) nextMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx + 1) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

// prevMatch cycles to the previous match and scrolls the viewport.
func (s *searchState) prevMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx - 1 + len(s.matchLines)) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

// scrollPercent returns the current viewport scroll position as 0-100.
func scrollPercent(vp *viewport.Model) int {
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

// Model is a Bubble Tea model for viewing rendered markdown.
// It supports single-file mode (no picker) and multi-file mode (picker + viewer).
type Model struct {
	pages         []Page
	picker        list.Model
	vp            viewport.Model
	level         level
	multi         bool // true when multiple pages
	current       int  // index of currently viewed page
	rendered      string
	renderedWidth int
	renderedPage  int // index of page used for cached render
	width         int
	height        int
	ready         bool
	quitting      bool
	search        searchState
}

// New creates a Model for the given pages.
// With one page it opens directly into the viewer; with multiple it shows a picker.
func New(pages []Page) *Model {
	m := &Model{
		pages:  pages,
		multi:  len(pages) > 1,
		search: newSearchState(),
	}
	if m.multi {
		m.level = levelPicker
	} else {
		m.level = levelViewer
	}
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.multi {
			m.picker.SetSize(msg.Width, msg.Height)
		}
		if m.ready {
			m.vp.SetWidth(msg.Width)
			m.vp.SetHeight(msg.Height - 1) // reserve status line
		} else if m.level == levelViewer {
			m.initViewport()
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.level {
		case levelViewer:
			return m.updateViewer(msg)
		default:
			return m.updatePicker(msg)
		}
	}

	if m.level == levelViewer && m.ready {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	if m.multi {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.picker.FilterState() == list.Filtering && msg.String() == "q" {
			break
		}
		m.quitting = true
		return m, tea.Quit
	case "enter":
		if m.picker.FilterState() == list.Filtering {
			break
		}
		idx := m.picker.Index()
		m.current = idx
		m.level = levelViewer
		m.search.clear(&m.vp, m.rendered)
		m.initViewport()
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m *Model) updateViewer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.search.searching {
		return m.updateModelSearch(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q", "esc":
		if m.multi {
			m.level = levelPicker
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "/", "f":
		return m, m.search.focus()
	case "n":
		m.search.nextMatch(&m.vp)
		return m, nil
	case "N":
		m.search.prevMatch(&m.vp)
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) updateModelSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.search.confirm(&m.vp, m.rendered)
		return m, nil
	case "esc":
		m.search.clear(&m.vp, m.rendered)
		return m, nil
	}

	return m, m.search.liveUpdate(msg, &m.vp, m.rendered)
}

func (m *Model) initViewport() {
	if m.width == 0 || m.height == 0 {
		return
	}

	contentW := m.width - 2 // breathing room
	if contentW < 20 {
		contentW = 20
	}

	if m.rendered == "" || m.renderedWidth != contentW || m.renderedPage != m.current {
		rendered, err := Render(m.pages[m.current].Content, contentW)
		if err != nil {
			rendered = m.pages[m.current].Content // fallback to raw
		}
		m.rendered = strings.TrimRight(rendered, "\n")
		m.renderedWidth = contentW
		m.renderedPage = m.current
	}

	m.vp = viewport.New(
		viewport.WithWidth(m.width),
		viewport.WithHeight(m.height-1), // reserve status line
	)
	m.vp.SoftWrap = true
	m.vp.SetContent(m.rendered)
	m.ready = true
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.level == levelPicker {
		v := tea.NewView(m.picker.View())
		v.AltScreen = true
		return v
	}

	if !m.ready {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	var b strings.Builder
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	if m.search.searching {
		b.WriteString(m.search.input.View())
	} else {
		b.WriteString(m.statusLine())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *Model) statusLine() string {
	pct := scrollPercent(&m.vp)

	title := m.pages[m.current].Name
	left := ui.TUI.AccentBold().Render(" " + title + " ")

	nav := "/ search  q quit"
	if m.multi {
		nav = "/ search  esc back  q quit"
	}
	if m.search.query != "" && !m.search.searching {
		nav = fmt.Sprintf("n/N next/prev (%d matches)  ", m.search.matchCount) + nav
	}
	right := ui.TUI.HeaderHint().Render(nav)

	scrollInfo := ui.TUI.Dim().Render(fmt.Sprintf("  %d%%", pct))

	return left + "  " + right + scrollInfo
}

// ── Embeddable sub-model interface ─────────────────────────────────────────

// Viewer is a minimal sub-model for embedding in other TUIs (e.g. guide overlay).
// Unlike Model, it has no picker or program control — just viewport + rendering.
type Viewer struct {
	vp            viewport.Model
	content       string // raw markdown
	rendered      string // cached glamour output
	renderedWidth int    // width used for cached render
	name          string
	ready         bool
	search        searchState
}

// NewViewer creates a viewer for a single markdown document.
func NewViewer(name, markdown string) *Viewer {
	return &Viewer{
		name:    name,
		content: markdown,
		search:  newSearchState(),
	}
}

// SetSize configures the viewport dimensions and re-renders content.
func (v *Viewer) SetSize(w, h int) {
	contentW := w - 2
	if contentW < 20 {
		contentW = 20
	}

	if v.rendered == "" || v.renderedWidth != contentW {
		rendered, err := Render(v.content, contentW)
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
func (v *Viewer) Searching() bool { return v.search.searching }

// MatchCount returns the number of search matches.
func (v *Viewer) MatchCount() int { return v.search.matchCount }

// Query returns the current search query.
func (v *Viewer) Query() string { return v.search.query }

// Update handles key and scroll messages.
func (v *Viewer) Update(msg tea.Msg) (*Viewer, tea.Cmd) {
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

func (v *Viewer) updateSearch(msg tea.Msg) (*Viewer, tea.Cmd) {
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

// applySearch finds all case-insensitive matches of query in rendered,
// updates the viewport content with highlights, and returns the match state.
func applySearch(query, rendered string, vp *viewport.Model) (matchLines []int, matchCount, matchIdx int) {
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

// View renders the viewport content, with search input appended when active.
func (v *Viewer) View() string {
	if !v.ready {
		return ""
	}
	if v.search.searching {
		return v.vp.View() + "\n" + v.search.input.View()
	}
	return v.vp.View()
}

// RawContent returns the original markdown source.
func (v *Viewer) RawContent() string { return v.content }

// ScrollPercent returns the current scroll position as 0-100.
func (v *Viewer) ScrollPercent() int {
	if !v.ready {
		return 0
	}
	return scrollPercent(&v.vp)
}
