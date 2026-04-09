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

// list.DefaultItem for the file picker.
func (p Page) Title() string       { return p.Name }
func (p Page) Description() string { return "" }
func (p Page) FilterValue() string { return p.Name }

// level tracks navigation state.
type level int

const (
	levelPicker level = iota // file picker (multi-file mode)
	levelViewer              // rendered markdown viewport
)

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
	width         int
	height        int
	ready         bool
	quitting      bool

	// Search state
	searching   bool
	searchInput textinput.Model
	query       string
	matchCount  int
	matchLines  []int
	matchIdx    int
}

// New creates a Model for the given pages.
// With one page it opens directly into the viewer; with multiple it shows a picker.
func New(pages []Page) *Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	m := &Model{
		pages:       pages,
		multi:       len(pages) > 1,
		searchInput: ti,
	}
	if m.multi {
		m.level = levelPicker
	} else {
		m.level = levelViewer
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return nil
}

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
		m.initViewport()
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m *Model) updateViewer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.searching {
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
		m.searching = true
		m.searchInput.SetValue("")
		return m, m.searchInput.Focus()
	case "n":
		if len(m.matchLines) > 0 {
			m.matchIdx = (m.matchIdx + 1) % len(m.matchLines)
			m.vp.SetYOffset(m.matchLines[m.matchIdx])
		}
		return m, nil
	case "N":
		if len(m.matchLines) > 0 {
			m.matchIdx = (m.matchIdx - 1 + len(m.matchLines)) % len(m.matchLines)
			m.vp.SetYOffset(m.matchLines[m.matchIdx])
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) updateModelSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.query = m.searchInput.Value()
		m.applyModelSearch()
		return m, nil
	case "esc":
		m.searching = false
		m.query = ""
		m.matchCount = 0
		m.matchLines = nil
		m.matchIdx = 0
		m.vp.SetContent(m.rendered)
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)

	q := m.searchInput.Value()
	if q != m.query {
		m.query = q
		m.applyModelSearch()
	}
	return m, cmd
}

func (m *Model) applyModelSearch() {
	if m.query == "" {
		m.matchCount = 0
		m.matchLines = nil
		m.matchIdx = 0
		m.vp.SetContent(m.rendered)
		return
	}

	plain := ansi.Strip(m.rendered)
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(m.query)

	m.matchLines = nil
	start := 0
	for {
		idx := strings.Index(lowerPlain[start:], lowerQuery)
		if idx < 0 {
			break
		}
		begin := start + idx
		line := strings.Count(lowerPlain[:begin], "\n")
		m.matchLines = append(m.matchLines, line)
		start = begin + len(m.query)
	}

	m.matchCount = len(m.matchLines)
	m.matchIdx = 0

	if m.matchCount > 0 {
		m.vp.SetContent(HighlightMatches(m.rendered, m.query))
		m.vp.SetYOffset(m.matchLines[0])
	} else {
		m.vp.SetContent(m.rendered)
	}
}

func (m *Model) initViewport() {
	if m.width == 0 || m.height == 0 {
		return
	}

	contentW := m.width - 2 // breathing room
	if contentW < 20 {
		contentW = 20
	}

	if m.rendered == "" || m.renderedWidth != contentW {
		rendered, err := Render(m.pages[m.current].Content, contentW)
		if err != nil {
			rendered = m.pages[m.current].Content // fallback to raw
		}
		m.rendered = strings.TrimRight(rendered, "\n")
		m.renderedWidth = contentW
	}

	m.vp = viewport.New(
		viewport.WithWidth(m.width),
		viewport.WithHeight(m.height-1), // reserve status line
	)
	m.vp.SoftWrap = true
	m.vp.SetContent(m.rendered)
	m.ready = true
}

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
	if m.searching {
		b.WriteString(m.searchInput.View())
	} else {
		b.WriteString(m.statusLine())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *Model) statusLine() string {
	pct := 0
	total := m.vp.TotalLineCount()
	visible := m.vp.VisibleLineCount()
	if total > visible {
		pct = (m.vp.YOffset() + visible) * 100 / total
		if pct > 100 {
			pct = 100
		}
	} else {
		pct = 100
	}

	title := m.pages[m.current].Name
	left := ui.TUI.AccentBold().Render(" " + title + " ")

	nav := "/ search  q quit"
	if m.multi {
		nav = "/ search  esc back  q quit"
	}
	if m.query != "" && !m.searching {
		nav = fmt.Sprintf("n/N next/prev (%d matches)  ", m.matchCount) + nav
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

	// Search state
	searching   bool
	searchInput textinput.Model
	query       string // last confirmed query
	matchCount  int
	matchLines  []int // line number for each match
	matchIdx    int   // current match index for n/N cycling
}

// NewViewer creates a viewer for a single markdown document.
func NewViewer(name, markdown string) *Viewer {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	return &Viewer{
		name:        name,
		content:     markdown,
		searchInput: ti,
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
func (v *Viewer) Searching() bool { return v.searching }

// MatchCount returns the number of search matches.
func (v *Viewer) MatchCount() int { return v.matchCount }

// Query returns the current search query.
func (v *Viewer) Query() string { return v.query }

// Update handles key and scroll messages.
func (v *Viewer) Update(msg tea.Msg) (*Viewer, tea.Cmd) {
	if !v.ready {
		return v, nil
	}

	if v.searching {
		return v.updateSearch(msg)
	}

	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "/", "f":
			v.searching = true
			v.searchInput.SetValue("")
			return v, v.searchInput.Focus()
		case "n":
			if len(v.matchLines) > 0 {
				v.matchIdx = (v.matchIdx + 1) % len(v.matchLines)
				v.vp.SetYOffset(v.matchLines[v.matchIdx])
			}
			return v, nil
		case "N":
			if len(v.matchLines) > 0 {
				v.matchIdx = (v.matchIdx - 1 + len(v.matchLines)) % len(v.matchLines)
				v.vp.SetYOffset(v.matchLines[v.matchIdx])
			}
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
			v.searching = false
			v.query = v.searchInput.Value()
			v.applySearch()
			return v, nil
		case "esc":
			v.searching = false
			v.query = ""
			v.matchCount = 0
			v.matchLines = nil
			v.matchIdx = 0
			v.vp.SetContent(v.rendered)
			return v, nil
		}
	}

	var cmd tea.Cmd
	v.searchInput, cmd = v.searchInput.Update(msg)

	// Live-highlight as user types.
	q := v.searchInput.Value()
	if q != v.query {
		v.query = q
		v.applySearch()
	}
	return v, cmd
}

func (v *Viewer) applySearch() {
	if v.query == "" {
		v.matchCount = 0
		v.matchLines = nil
		v.matchIdx = 0
		v.vp.SetContent(v.rendered)
		return
	}

	plain := ansi.Strip(v.rendered)
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(v.query)

	v.matchLines = nil
	start := 0
	for {
		idx := strings.Index(lowerPlain[start:], lowerQuery)
		if idx < 0 {
			break
		}
		begin := start + idx
		line := strings.Count(lowerPlain[:begin], "\n")
		v.matchLines = append(v.matchLines, line)
		start = begin + len(v.query)
	}

	v.matchCount = len(v.matchLines)
	v.matchIdx = 0

	if v.matchCount > 0 {
		v.vp.SetContent(HighlightMatches(v.rendered, v.query))
		v.vp.SetYOffset(v.matchLines[0])
	} else {
		v.vp.SetContent(v.rendered)
	}
}

// View renders the viewport content, with search input appended when active.
func (v *Viewer) View() string {
	if !v.ready {
		return ""
	}
	if v.searching {
		return v.vp.View() + "\n" + v.searchInput.View()
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
	total := v.vp.TotalLineCount()
	visible := v.vp.VisibleLineCount()
	if total <= visible {
		return 100
	}
	pct := (v.vp.YOffset() + visible) * 100 / total
	if pct > 100 {
		pct = 100
	}
	return pct
}
