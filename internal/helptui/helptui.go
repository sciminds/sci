// Package helptui provides an interactive help TUI that lets users browse
// commands and watch embedded demo recordings, used by "sci help".
package helptui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/guide"
	"github.com/sciminds/cli/internal/ui"
)

// level tracks navigation depth.
type level int

const (
	levelCommands level = iota // top-level command picker
	levelSubs                  // subcommand list within a command
	levelOverlay               // cast player overlay
)

// model is the Bubble Tea model for the interactive help TUI.
type model struct {
	groups   []CommandGroup
	commands list.Model // level 0: command picker
	subs     list.Model // level 1: subcommand list
	player   *guide.Player
	level    level
	group    *CommandGroup // current group at level 1+
	width    int
	height   int
	quitting bool
}

func newModel(groups []CommandGroup) *model {
	items := make([]list.Item, len(groups))
	for i, g := range groups {
		items[i] = g
	}

	d := ui.NewListDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = "Commands"
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return &model{groups: groups, commands: l, level: levelCommands}
}

func newModelForGroup(g *CommandGroup) *model {
	m := &model{
		groups: []CommandGroup{*g},
		level:  levelSubs,
	}
	m.openGroup(*g)
	return m
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		switch m.level {
		case levelCommands:
			m.commands.SetSize(msg.Width, msg.Height)
		case levelSubs, levelOverlay:
			m.subs.SetSize(msg.Width, msg.Height-m.descHeight())
		}
		if m.player != nil {
			m.player.SetHeight(ui.OverlayBodyHeight(m.height, 4))
			m.player.SetWidth(ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW) - ui.OverlayBoxPadding)
		}
		return m, nil

	case guide.TickMsg:
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
		case levelSubs:
			return m.updateSubs(msg)
		default:
			return m.updateCommands(msg)
		}
	}

	var cmd tea.Cmd
	switch m.level {
	case levelSubs:
		m.subs, cmd = m.subs.Update(msg)
	default:
		m.commands, cmd = m.commands.Update(msg)
	}
	return m, cmd
}

// ── Level 0: command picker ────────────────────────────────────────────────

func (m *model) updateCommands(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.commands.FilterState() != list.Filtering {
			m.quitting = true
			return m, tea.Quit
		}
	case "enter":
		if m.commands.FilterState() == list.Filtering {
			break
		}
		g, ok := m.commands.SelectedItem().(CommandGroup)
		if !ok {
			break
		}
		m.openGroup(g)
		return m, nil
	}

	var cmd tea.Cmd
	m.commands, cmd = m.commands.Update(msg)
	return m, cmd
}

func (m *model) openGroup(g CommandGroup) {
	items := make([]list.Item, len(g.Subs))
	for i, s := range g.Subs {
		items[i] = s
	}
	m.group = &g
	d := ui.NewListDelegate()
	listH := m.height - m.descHeight()
	l := list.New(items, d, m.width, listH)
	l.Title = g.Name
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play demo")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	}
	m.subs = l
	m.level = levelSubs
}

// ── Level 1: subcommand list ───────────────────────────────────────────────

func (m *model) updateSubs(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.subs.FilterState() != list.Filtering {
			return m.goBack()
		}
	case "esc":
		if m.subs.FilterState() != list.Filtering {
			return m.goBack()
		}
	case "enter":
		if m.subs.FilterState() == list.Filtering {
			break
		}
		sub, ok := m.subs.SelectedItem().(SubCommand)
		if !ok || sub.CastFile == "" {
			m.subs.NewStatusMessage("no demo available")
			break
		}
		data, err := loadCast(sub.CastFile)
		if err != nil {
			m.subs.NewStatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		cast, err := guide.ParseCast(data)
		if err != nil {
			m.subs.NewStatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		visH := ui.OverlayBodyHeight(m.height, 4)
		visW := ui.OverlayWidth(m.width, ui.OverlayMinW, ui.OverlayMaxW) - ui.OverlayBoxPadding
		m.player = guide.NewPlayer(cast, visH)
		m.player.SetWidth(visW)
		m.level = levelOverlay
		return m, m.player.Init()
	}

	var cmd tea.Cmd
	m.subs, cmd = m.subs.Update(msg)
	return m, cmd
}

func (m *model) goBack() (tea.Model, tea.Cmd) {
	if len(m.groups) == 1 {
		m.quitting = true
		return m, tea.Quit
	}
	m.level = levelCommands
	return m, nil
}

// ── Level 2: cast overlay ──────────────────────────────────────────────────

func (m *model) updateOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.player = nil
		m.level = levelSubs
		return m, nil
	}

	var cmd tea.Cmd
	m.player, cmd = m.player.Update(msg)
	return m, cmd
}

// ── Description block ──────────────────────────────────────────────────────

// descMaxWidth caps the description paragraph so it doesn't stretch across
// ultra-wide terminals.
const descMaxWidth = 76

// renderDesc returns the styled description block for the current group,
// or "" if there is no long description.
func (m *model) renderDesc() string {
	if m.group == nil || m.group.LongDesc == "" {
		return ""
	}
	w := m.width - 4 // account for list padding
	if w > descMaxWidth {
		w = descMaxWidth
	}
	if w < 20 {
		w = 20
	}
	body := ui.TUI.TextMid().Width(w).Render(m.group.LongDesc)
	// one blank line below the description to separate from items
	return body + "\n"
}

// descHeight returns the number of terminal rows the description block
// occupies, including its trailing blank line. Returns 0 when there is no
// description.
func (m *model) descHeight() int {
	d := m.renderDesc()
	if d == "" {
		return 0
	}
	return lipgloss.Height(d)
}

// ── View ───────────────────────────────────────────────────────────────────

func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var bg string
	switch m.level {
	case levelCommands:
		bg = m.commands.View()
	case levelSubs, levelOverlay:
		if desc := m.renderDesc(); desc != "" {
			bg = desc + m.subs.View()
		} else {
			bg = m.subs.View()
		}
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

	sub, _ := m.subs.SelectedItem().(SubCommand)
	b.WriteString(ui.TUI.HeaderSection().Render(" " + sub.Name + " "))
	b.WriteString("\n\n")

	b.WriteString(m.player.View())
	b.WriteString("\n\n")

	hints := []string{
		ui.TUI.HeaderHint().Render("space pause/play"),
		ui.TUI.HeaderHint().Render("r restart"),
		ui.TUI.HeaderHint().Render("esc close"),
	}
	b.WriteString(strings.Join(hints, "  "))

	return ui.TUI.OverlayBox().
		Width(w).
		Render(b.String())
}

// ── Public API ─────────────────────────────────────────────────────────────

// Run launches the interactive help TUI showing all command groups.
func Run(groups []CommandGroup) error {
	m := newModel(groups)
	p := tea.NewProgram(m)
	_, err := p.Run()
	ui.DrainStdin()
	return err
}

// RunGroup launches the help TUI for a single command group, skipping the
// top-level picker.
func RunGroup(g *CommandGroup) error {
	m := newModelForGroup(g)
	p := tea.NewProgram(m)
	_, err := p.Run()
	ui.DrainStdin()
	return err
}
