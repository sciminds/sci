package guide

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/ui"
)

// level tracks where we are in the navigation hierarchy.
type level int

const (
	levelBooks   level = iota // top-level book picker
	levelEntries              // entry list within a book
	levelOverlay              // cast player overlay
)

// model is the top-level Bubble Tea model for the guide TUI.
type model struct {
	books    list.Model // top-level book picker
	entries  list.Model // entry list for the selected book
	player   *Player
	level    level
	width    int
	height   int
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

	return &model{books: l, level: levelBooks}
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
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play demo")),
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

func (m *model) updateOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.player = nil
		m.level = levelEntries
		return m, nil
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

	if m.player == nil {
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

	// Player content
	b.WriteString(m.player.View())
	b.WriteString("\n\n")

	// Footer keybinds
	footer := ui.TUI.HeaderHint().Render("space pause/play") + "  " +
		ui.TUI.HeaderHint().Render("r restart") + "  " +
		ui.TUI.HeaderHint().Render("esc close")
	b.WriteString(footer)

	return ui.TUI.OverlayBox().
		Width(w).
		Render(b.String())
}

// Run launches the interactive guide TUI with the given books.
func Run(books []Book) error {
	m := newModel(books)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
