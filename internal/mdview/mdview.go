package mdview

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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
}

// New creates a Model for the given pages.
// With one page it opens directly into the viewer; with multiple it shows a picker.
func New(pages []Page) *Model {
	m := &Model{
		pages: pages,
		multi: len(pages) > 1,
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
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
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
	b.WriteString(m.statusLine())

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

	nav := "q quit"
	if m.multi {
		nav = "esc back  q quit"
	}
	right := ui.TUI.HeaderHint().Render(nav)

	scrollInfo := ui.TUI.Dim().Render(
		strings.Repeat(" ", 2) + string(rune('0'+pct/100%10)) +
			string(rune('0'+pct/10%10)) +
			string(rune('0'+pct%10)) + "%",
	)

	return left + "  " + right + "  " + scrollInfo
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
}

// NewViewer creates a viewer for a single markdown document.
func NewViewer(name, markdown string) *Viewer {
	return &Viewer{
		name:    name,
		content: markdown,
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

// Update handles key and scroll messages.
func (v *Viewer) Update(msg tea.Msg) (*Viewer, tea.Cmd) {
	if !v.ready {
		return v, nil
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return v, cmd
}

// View renders the viewport content.
func (v *Viewer) View() string {
	if !v.ready {
		return ""
	}
	return v.vp.View()
}

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
