package guide

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/ui"
)

// model is the top-level Bubble Tea model for the guide TUI.
type model struct {
	list     list.Model
	player   *Player // nil when overlay is closed
	width    int
	height   int
	quitting bool
}

func newModel(title string, entries []Entry) *model {
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = e
	}

	d := ui.NewListDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = title
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play demo")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return &model{list: l}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height)
		if m.player != nil {
			m.player.SetHeight(ui.OverlayBodyHeight(m.height, 4))
		}
		return m, nil

	case tickMsg:
		if m.player != nil {
			var cmd tea.Cmd
			m.player, cmd = m.player.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.player != nil {
			return m.updateOverlay(msg)
		}
		return m.updateList(msg)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.list.FilterState() != list.Filtering {
			m.quitting = true
			return m, tea.Quit
		}
	case "enter":
		if m.list.FilterState() == list.Filtering {
			break
		}
		item, ok := m.list.SelectedItem().(Entry)
		if !ok {
			break
		}
		data, err := LoadCast(item.CastFile)
		if err != nil {
			m.list.NewStatusMessage(fmt.Sprintf("Error loading %s: %v", item.CastFile, err))
			break
		}
		cast, err := ParseCast(data)
		if err != nil {
			m.list.NewStatusMessage(fmt.Sprintf("Error parsing %s: %v", item.CastFile, err))
			break
		}
		visH := ui.OverlayBodyHeight(m.height, 4)
		m.player = NewPlayer(cast, visH)
		return m, m.player.Init()
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) updateOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.player = nil
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

	bg := m.list.View()
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
	entry, _ := m.list.SelectedItem().(Entry)
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

// Run launches the interactive guide TUI with the given title and entries.
func Run(title string, entries []Entry) error {
	m := newModel(title, entries)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
