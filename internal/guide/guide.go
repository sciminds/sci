package guide

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/mdview"
	"github.com/sciminds/cli/internal/ui"
)

// level tracks where we are in the navigation hierarchy.
type level int

const (
	levelBooks   level = iota // top-level book picker
	levelEntries              // entry list within a book
	levelOverlay              // cast player overlay
)

// pagesWarmedMsg signals that background page pre-rendering is complete.
type pagesWarmedMsg struct{}

// model is the top-level Bubble Tea model for the guide TUI.
type model struct {
	allBooks []Book     // original book data (for pre-rendering)
	books    list.Model // top-level book picker
	entries  list.Model // entry list for the selected book
	player   *Player
	viewer   *mdview.Viewer // markdown page viewer
	level    level
	width    int
	height   int
	warmed   bool // true after pre-render cmd fired
	quitting bool
}

func newModel(books []Book) *model {
	items := make([]list.Item, len(books))
	for i, b := range books {
		items[i] = b
	}

	d := ui.NewListDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = "Guides"
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return &model{allBooks: books, books: l, level: levelBooks}
}

func (m *model) Init() tea.Cmd {
	return nil
}

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
			m.player.SetHeight(ui.OverlayBodyHeight(m.height, 4))
		}
		if m.viewer != nil {
			w := ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW) - ui.OverlayBoxPadding
			m.viewer.SetSize(w, ui.OverlayBodyHeight(m.height, 4))
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
			m.entries.NewStatusMessage(fmt.Sprintf("Export failed: %v", msg.err))
		} else {
			m.entries.NewStatusMessage(fmt.Sprintf("Exported to %s", msg.path))
		}
		return m, nil

	case TickMsg:
		if m.player != nil {
			var cmd tea.Cmd
			m.player, cmd = m.player.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.level {
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
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.books.FilterState() != list.Filtering {
			m.quitting = true
			return m, tea.Quit
		}
	case "enter":
		if m.books.FilterState() == list.Filtering {
			break
		}
		book, ok := m.books.SelectedItem().(Book)
		if !ok {
			break
		}
		m.openBook(book)
		return m, nil
	}

	var cmd tea.Cmd
	m.books, cmd = m.books.Update(msg)
	return m, cmd
}

func (m *model) openBook(book Book) {
	items := make([]list.Item, len(book.Entries))
	for i, e := range book.Entries {
		items[i] = e
	}
	d := ui.NewListDelegate()
	l := list.New(items, d, m.width, m.height)
	l.Title = book.Heading
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	}
	m.entries = l
	m.level = levelEntries
}

func (m *model) updateEntries(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.entries.FilterState() != list.Filtering {
			m.level = levelBooks
			return m, nil
		}
	case "esc":
		if m.entries.FilterState() != list.Filtering {
			m.level = levelBooks
			return m, nil
		}
	case "enter":
		if m.entries.FilterState() == list.Filtering {
			break
		}
		item, ok := m.entries.SelectedItem().(Entry)
		if !ok {
			break
		}
		if item.PageFile != "" {
			return m.openPage(item)
		}
		data, err := LoadCast(item.CastFile)
		if err != nil {
			m.entries.NewStatusMessage(fmt.Sprintf("Error loading %s: %v", item.CastFile, err))
			break
		}
		cast, err := ParseCast(data)
		if err != nil {
			m.entries.NewStatusMessage(fmt.Sprintf("Error parsing %s: %v", item.CastFile, err))
			break
		}
		visH := ui.OverlayBodyHeight(m.height, 4)
		m.player = NewPlayer(cast, visH)
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
		m.entries.NewStatusMessage(fmt.Sprintf("Error loading %s: %v", item.PageFile, err))
		return m, nil
	}
	v := mdview.NewViewer(item.Cmd, string(data))
	w := ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW) - ui.OverlayBoxPadding
	v.SetSize(w, ui.OverlayBodyHeight(m.height, 4))
	m.viewer = v
	m.level = levelOverlay
	return m, nil
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

func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
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
	v := tea.NewView(ui.Compose(fg, bg))
	v.AltScreen = true
	return v
}

func (m *model) renderOverlay() string {
	w := ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW)

	var b strings.Builder

	// Title
	entry, _ := m.entries.SelectedItem().(Entry)
	title := entry.Cmd
	b.WriteString(ui.TUI.HeaderSection().Render(" " + title + " "))
	b.WriteString("\n\n")

	if m.viewer != nil {
		b.WriteString(m.viewer.View())
		b.WriteString("\n\n")

		if m.viewer.Searching() {
			// No footer during search — the input is visible in the viewer.
		} else {
			pct := fmt.Sprintf("%d%%", m.viewer.ScrollPercent())
			hints := ui.TUI.HeaderHint().Render("↑/↓ scroll") + "  " +
				ui.TUI.HeaderHint().Render("/ search") + "  " +
				ui.TUI.HeaderHint().Render("e export") + "  " +
				ui.TUI.HeaderHint().Render("q/esc close")
			if m.viewer.Query() != "" {
				hints += "  " + ui.TUI.HeaderHint().Render(
					fmt.Sprintf("n/N next/prev (%d matches)", m.viewer.MatchCount()),
				)
			}
			hints += "  " + ui.TUI.Dim().Render(pct)
			b.WriteString(hints)
		}
	} else {
		b.WriteString(m.player.View())
		b.WriteString("\n\n")

		footer := ui.TUI.HeaderHint().Render("space pause/play") + "  " +
			ui.TUI.HeaderHint().Render("r restart") + "  " +
			ui.TUI.HeaderHint().Render("esc close")
		b.WriteString(footer)
	}

	return ui.TUI.OverlayBox().
		Width(w).
		Render(b.String())
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
	return func() tea.Msg {
		err := os.WriteFile(filename, []byte(content), 0o644)
		return exportedMsg{path: filename, err: err}
	}
}

// preRenderPages returns a Cmd that renders all page-based entries in the
// background so they're cached by the time the user opens them.
func (m *model) preRenderPages() tea.Cmd {
	contentW := ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW) - ui.OverlayBoxPadding - 2
	if contentW < 20 {
		contentW = 20
	}

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

	return func() tea.Msg {
		mdview.PreRender(docs, contentW)
		return pagesWarmedMsg{}
	}
}

// Run launches the interactive guide TUI with the given books.
func Run(books []Book) error {
	mdview.DetectStyle() // probe terminal before bubbletea takes over stdin
	m := newModel(books)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
