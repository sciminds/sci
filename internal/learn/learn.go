// Package learn provides an interactive tutorial TUI with embedded asciicast
// playback and markdown page viewing, used by "sci learn".
package learn

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// level tracks where we are in the navigation hierarchy.
type level int

const (
	levelBooks   level = iota // top-level book picker
	levelEntries              // entry list within a book
	levelOverlay              // cast player or page viewer overlay
	levelSplit                // side-by-side markdown + cast
)

// pagesWarmedMsg signals that background page pre-rendering is complete.
type pagesWarmedMsg struct{}

// model is the top-level Bubble Tea model for the learn TUI.
type model struct {
	allBooks []Book           // original book data (for pre-rendering)
	books    uikit.ListPicker // top-level book picker
	entries  uikit.ListPicker // entry list for the selected book
	player   *uikit.CastPlayer
	viewer   *uikit.MdViewer  // markdown page viewer
	split    *uikit.SplitView // side-by-side markdown + cast
	level    level
	width    int
	height   int
	warmed   bool // true after pre-render cmd fired
	quitting bool
}

func newModel(books []Book) *model {
	lp := uikit.NewListPicker("Guides", uikit.Items(books))
	return &model{allBooks: books, books: lp, level: levelBooks}
}

// Init implements tea.Model.
func (m *model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.books.SetSize(msg.Width, msg.Height)
		if m.level >= levelEntries {
			m.entries.SetSize(msg.Width, msg.Height)
		}
		if m.player != nil {
			m.player.SetHeight(uikit.OverlayBodyHeight(m.height, 4))
			m.player.SetWidth(uikit.OverlayContentWidth(m.width))
		}
		if m.viewer != nil {
			w := uikit.OverlayContentWidth(m.width)
			m.viewer.SetSize(w, uikit.OverlayBodyHeight(m.height, 4))
		}
		if m.split != nil {
			m.split.SetSize(msg.Width, msg.Height)
		}
		if !m.warmed {
			m.warmed = true
			return m, m.preRenderPages()
		}
		return m, nil

	case pagesWarmedMsg:
		return m, nil

	case exportedMsg:
		if msg.err != nil {
			m.entries.StatusMessage(fmt.Sprintf("Export failed: %v", msg.err))
		} else {
			m.entries.StatusMessage(fmt.Sprintf("Exported to %s", msg.path))
		}
		return m, nil

	case uikit.CastTickMsg:
		if m.split != nil {
			var cmd tea.Cmd
			m.split, cmd = m.split.Update(msg)
			return m, cmd
		}
		if m.player != nil {
			var cmd tea.Cmd
			m.player, cmd = m.player.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.level {
		case levelSplit:
			return m.updateSplit(msg)
		case levelOverlay:
			return m.updateOverlay(msg)
		case levelEntries:
			return m.updateEntries(msg)
		default:
			return m.updateBooks(msg)
		}
	}

	// Delegate to the active list for non-key messages.
	var cmd tea.Cmd
	switch m.level {
	case levelEntries:
		m.entries, cmd = m.entries.Update(msg)
	default:
		m.books, cmd = m.books.Update(msg)
	}
	return m, cmd
}

func (m *model) updateBooks(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Navigation intent comes from the shared keymap — enter/l/right open,
	// q/ctrl+c quit — so books behave like every other sci list.
	switch m.books.Classify(msg) {
	case uikit.IntentQuit:
		m.quitting = true
		return m, tea.Quit
	case uikit.IntentOpen:
		if book, ok := m.books.SelectedItem().(Book); ok {
			m.openBook(book)
		}
		return m, nil
	case uikit.IntentBack:
		// At the root there is nothing above to go back to.
		return m, nil
	}

	var cmd tea.Cmd
	m.books, cmd = m.books.Update(msg)
	return m, cmd
}

func (m *model) openBook(book Book) {
	m.entries = uikit.NewListPicker(book.Heading, uikit.Items(book.Entries))
	m.entries.SetSize(m.width, m.height)
	m.level = levelEntries
}

func (m *model) updateEntries(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ctrl+c hard-quits from any depth; q/esc/h here step back to the book
	// list (IntentBack and the soft IntentQuit both mean "back" when nested).
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	switch m.entries.Classify(msg) {
	case uikit.IntentBack, uikit.IntentQuit:
		m.level = levelBooks
		return m, nil
	case uikit.IntentOpen:
		item, ok := m.entries.SelectedItem().(Entry)
		if !ok {
			break
		}
		// Both files → side-by-side split view.
		if item.CastFile != "" && item.PageFile != "" {
			return m.openSplit(item)
		}
		// Page only → markdown overlay.
		if item.PageFile != "" {
			return m.openPage(item)
		}
		// Cast only → player overlay.
		data, err := LoadCast(item.CastFile)
		if err != nil {
			m.entries.StatusMessage(fmt.Sprintf("Error loading %s: %v", item.CastFile, err))
			break
		}
		cast, err := uikit.ParseCast(data)
		if err != nil {
			m.entries.StatusMessage(fmt.Sprintf("Error parsing %s: %v", item.CastFile, err))
			break
		}
		visH := uikit.OverlayBodyHeight(m.height, 4)
		visW := uikit.OverlayContentWidth(m.width)
		m.player = uikit.NewCastPlayer(cast, visH)
		m.player.SetWidth(visW)
		m.level = levelOverlay
		return m, m.player.Init()
	}

	var cmd tea.Cmd
	m.entries, cmd = m.entries.Update(msg)
	return m, cmd
}

func (m *model) openPage(item Entry) (tea.Model, tea.Cmd) {
	data, err := LoadPage(item.PageFile)
	if err != nil {
		m.entries.StatusMessage(fmt.Sprintf("Error loading %s: %v", item.PageFile, err))
		return m, nil
	}
	v := uikit.NewMdViewer(item.Cmd, string(data))
	w := uikit.OverlayContentWidth(m.width)
	v.SetSize(w, uikit.OverlayBodyHeight(m.height, 4))
	m.viewer = v
	m.level = levelOverlay
	return m, nil
}

func (m *model) openSplit(item Entry) (tea.Model, tea.Cmd) {
	pageData, err := LoadPage(item.PageFile)
	if err != nil {
		m.entries.StatusMessage(fmt.Sprintf("Error loading %s: %v", item.PageFile, err))
		return m, nil
	}
	castData, err := LoadCast(item.CastFile)
	if err != nil {
		m.entries.StatusMessage(fmt.Sprintf("Error loading %s: %v", item.CastFile, err))
		return m, nil
	}
	cast, err := uikit.ParseCast(castData)
	if err != nil {
		m.entries.StatusMessage(fmt.Sprintf("Error parsing %s: %v", item.CastFile, err))
		return m, nil
	}

	viewer := uikit.NewMdViewer(item.Cmd, string(pageData))
	player := uikit.NewCastPlayer(cast, 10) // height set by SetSize below
	s := newSplitView(item.Cmd, viewer, player)
	s.SetSize(m.width, m.height)
	m.split = s
	m.level = levelSplit
	return m, s.Init()
}

func (m *model) updateSplit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// When the viewer is in search mode, delegate everything except ctrl+c.
	if m.split.Searching() {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.split, cmd = m.split.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.split = nil
		m.level = levelEntries
		return m, nil
	}

	var cmd tea.Cmd
	m.split, cmd = m.split.Update(msg)
	return m, cmd
}

func (m *model) updateOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// When the viewer is in search mode, delegate everything except ctrl+c.
	if m.viewer != nil && m.viewer.Searching() {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.player = nil
		m.viewer = nil
		m.level = levelEntries
		return m, nil
	case "q":
		if m.viewer != nil {
			m.viewer = nil
			m.level = levelEntries
			return m, nil
		}
	case "e":
		if m.viewer != nil {
			entry, _ := m.entries.SelectedItem().(Entry)
			return m, m.exportPage(entry)
		}
	}

	if m.viewer != nil {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.player, cmd = m.player.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	// Split view is full-screen, no overlay compositing.
	if m.split != nil {
		v := tea.NewView(m.split.View())
		v.AltScreen = true
		return v
	}

	var bg string
	switch m.level {
	case levelEntries, levelOverlay:
		bg = m.entries.View()
	default:
		bg = m.books.View()
	}

	if m.player == nil && m.viewer == nil {
		v := tea.NewView(bg)
		v.AltScreen = true
		return v
	}

	fg := m.renderOverlay()
	v := tea.NewView(uikit.Compose(fg, bg))
	v.AltScreen = true
	return v
}

func (m *model) renderOverlay() string {
	entry, _ := m.entries.SelectedItem().(Entry)

	// Player-only overlay: clean fit for OverlayBox.
	if m.viewer == nil {
		return uikit.OverlayBox{
			Title: entry.Cmd,
			Body:  m.player.View(),
			Hints: []string{"space pause/play", "r restart", "esc close"},
		}.Render(m.width, m.height)
	}

	// Viewer overlay has conditional hints (search mode, match count,
	// scroll percentage with dim styling) — render manually.
	w := uikit.OverlayWidth(m.width, uikit.OverlayMinW, uikit.OverlayMaxW)

	var b strings.Builder
	b.WriteString(uikit.TUI.HeaderSection().Render(" " + entry.Cmd + " "))
	b.WriteString("\n\n")
	b.WriteString(m.viewer.View())

	if !m.viewer.Searching() {
		b.WriteString("\n\n")
		pct := fmt.Sprintf("%d%%", m.viewer.ScrollPercent())
		hints := uikit.TUI.HeaderHint().Render("↑/↓ scroll") + "  " +
			uikit.TUI.HeaderHint().Render("/ search") + "  " +
			uikit.TUI.HeaderHint().Render("e export") + "  " +
			uikit.TUI.HeaderHint().Render("q/esc close")
		if m.viewer.Query() != "" {
			hints += "  " + uikit.TUI.HeaderHint().Render(
				fmt.Sprintf("n/N next/prev (%d matches)", m.viewer.MatchCount()),
			)
		}
		hints += "  " + uikit.TUI.Dim().Render(pct)
		b.WriteString(hints)
	}

	return uikit.TUI.OverlayBox().Width(w).Render(b.String())
}

// exportedMsg carries the result of an export attempt.
type exportedMsg struct {
	path string
	err  error
}

// exportPage writes the raw markdown to the current directory.
func (m *model) exportPage(entry Entry) tea.Cmd {
	content := m.viewer.RawContent()
	filename := entry.PageFile
	return uikit.SafeCmd(func() tea.Msg {
		err := os.WriteFile(filename, []byte(content), 0o644)
		return exportedMsg{path: filename, err: err}
	})
}

// preRenderPages returns a Cmd that renders all page-based entries in the
// background so they're cached by the time the user opens them.
func (m *model) preRenderPages() tea.Cmd {
	contentW := max(uikit.OverlayContentWidth(m.width)-2, 20)

	var docs []string
	for _, book := range m.allBooks {
		for _, e := range book.Entries {
			if e.PageFile == "" {
				continue
			}
			data, err := LoadPage(e.PageFile)
			if err != nil {
				continue
			}
			docs = append(docs, string(data))
		}
	}
	if len(docs) == 0 {
		return nil
	}

	return uikit.SafeCmd(func() tea.Msg {
		uikit.PreRenderMarkdown(docs, contentW)
		return pagesWarmedMsg{}
	})
}

// Run launches the interactive learn TUI with the given books.
func Run(books []Book) error {
	uikit.DetectTermStyle() // probe terminal before bubbletea takes over stdin
	return uikit.Run(newModel(books))
}
