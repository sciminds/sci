// Package help provides an interactive help TUI that lets users browse
// commands and watch embedded demo recordings, used by "sci help".
package help

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/uikit"
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
	commands uikit.ListPicker // level 0: command picker
	subs     uikit.ListPicker // level 1: subcommand list
	player   *uikit.CastPlayer
	level    level
	group    *CommandGroup // current group at level 1+
	width    int
	height   int
	quitting bool
}

func newModel(groups []CommandGroup) *model {
	lp := uikit.NewListPicker("Commands", uikit.Items(groups),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)
	return &model{groups: groups, commands: lp, level: levelCommands}
}

func newModelForGroup(g *CommandGroup) *model {
	m := &model{
		groups: []CommandGroup{*g},
		level:  levelSubs,
	}
	m.openGroup(*g)
	return m
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
		switch m.level {
		case levelCommands:
			m.commands.SetSize(msg.Width, msg.Height)
		case levelSubs, levelOverlay:
			m.subs.SetSize(msg.Width, msg.Height-m.descHeight())
		}
		if m.player != nil {
			m.player.SetHeight(uikit.OverlayBodyHeight(m.height, 4))
			m.player.SetWidth(uikit.OverlayWidth(m.width, uikit.OverlayMinW, uikit.OverlayMaxW) - uikit.OverlayBoxPadding)
		}
		return m, nil

	case uikit.CastTickMsg:
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
		if !m.commands.IsFiltering() {
			m.quitting = true
			return m, tea.Quit
		}
	case "enter":
		if m.commands.IsFiltering() {
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
	m.group = &g
	m.subs = uikit.NewListPicker(g.Name, uikit.Items(g.Subs),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play demo")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	)
	m.subs.SetSize(m.width, m.height-m.descHeight())
	m.level = levelSubs
}

// ── Level 1: subcommand list ───────────────────────────────────────────────

func (m *model) updateSubs(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if !m.subs.IsFiltering() {
			return m.goBack()
		}
	case "esc":
		if !m.subs.IsFiltering() {
			return m.goBack()
		}
	case "enter":
		if m.subs.IsFiltering() {
			break
		}
		sub, ok := m.subs.SelectedItem().(SubCommand)
		if !ok || sub.CastFile == "" {
			m.subs.StatusMessage("no demo available")
			break
		}
		data, err := loadCast(sub.CastFile)
		if err != nil {
			m.subs.StatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		cast, err := uikit.ParseCast(data)
		if err != nil {
			m.subs.StatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		visH := uikit.OverlayBodyHeight(m.height, 4)
		visW := uikit.OverlayWidth(m.width, uikit.OverlayMinW, uikit.OverlayMaxW) - uikit.OverlayBoxPadding
		m.player = uikit.NewCastPlayer(cast, visH)
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
	body := uikit.TUI.TextMid().Width(w).Render(m.group.LongDesc)
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

// View implements tea.Model.
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
	v := tea.NewView(uikit.Compose(fg, bg))
	v.AltScreen = true
	return v
}

func (m *model) renderOverlay() string {
	sub, _ := m.subs.SelectedItem().(SubCommand)
	return uikit.OverlayBox{
		Title: sub.Name,
		Body:  m.player.View(),
		Hints: []string{"space pause/play", "r restart", "esc close"},
	}.Render(m.width)
}

// ── Public API ─────────────────────────────────────────────────────────────

// Run launches the interactive help TUI showing all command groups.
func Run(groups []CommandGroup) error {
	return uikit.Run(newModel(groups))
}

// RunGroup launches the help TUI for a single command group, skipping the
// top-level picker.
func RunGroup(g *CommandGroup) error {
	return uikit.Run(newModelForGroup(g))
}
