package doctor

// reccs_tui.go — interactive list TUI for optional tool recommendations.

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/ui"
)

// toolDescs maps tool names to user-friendly descriptions.
var toolDescs = map[string]string{
	"helix":              "Terminal text editor with modal editing and built-in LSP",
	"nvim":               "Highly extensible terminal text editor",
	"msedit":             "Quick file editing from the terminal via MS Edit",
	"starship":           "Minimal, fast, customizable shell prompt",
	"lsd":                "Modern ls replacement with colors and icons",
	"jq":                 "Lightweight command-line JSON processor",
	"mq":                 "jq for Markdown — query and transform .md files",
	"rg":                 "ripgrep — blazing fast recursive text search",
	"ast-grep":           "Structural code search and rewrite using AST patterns",
	"visual-studio-code": "Popular graphical code editor by Microsoft",
	"zed":                "High-performance graphical code editor",
	"symbex":             "Find Python symbols (functions, classes) from the CLI",
	"sqlite-utils":       "CLI tool for manipulating SQLite databases",
	"markitdown":         "Convert documents (PDF, DOCX, etc.) to Markdown",
	"datasette":          "Instant web UI and JSON API for SQLite databases",
}

// reccsItem implements list.Item for the bubbles list component.
type reccsItem struct {
	entry brew.BrewfileEntry
	desc  string
}

func (i reccsItem) Title() string       { return i.entry.Name }
func (i reccsItem) Description() string { return i.desc }
func (i reccsItem) FilterValue() string { return i.entry.Name + " " + i.desc }

// reccsModel is the Bubble Tea model for the recommendations list.
type reccsModel struct {
	list     list.Model
	entries  []brew.BrewfileEntry
	chosen   int // index into entries; -1 = nothing selected
	quitting bool
}

func newReccsModel(entries []brew.BrewfileEntry, missing map[string]bool) reccsModel {
	// Only show tools that are not yet installed.
	var filtered []brew.BrewfileEntry
	var items []list.Item
	for _, e := range entries {
		if !missing[e.Name] {
			continue // already installed — hide it
		}
		desc := toolDescs[e.Name]
		if desc == "" {
			desc = e.Type + " package"
		}
		desc += ui.TUI.Muted().Render("  " + e.Type)
		filtered = append(filtered, e)
		items = append(items, reccsItem{entry: e, desc: desc})
	}

	title := fmt.Sprintf("Recommended tools — %d available", len(items))
	delegate := ui.NewListDelegate()
	l := list.New(items, delegate, 80, 24)
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "install")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return reccsModel{list: l, entries: filtered, chosen: -1}
}

func (m reccsModel) Init() tea.Cmd {
	return nil
}

func (m reccsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if item, ok := m.list.SelectedItem().(reccsItem); ok {
				for i, e := range m.entries {
					if e.Name == item.entry.Name {
						m.chosen = i
						break
					}
				}
			}
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m reccsModel) View() tea.View {
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}
