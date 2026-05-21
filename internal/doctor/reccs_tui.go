package doctor

// reccs_tui.go — interactive list TUI for optional tool recommendations.

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
)

// toolDescs maps tool names to user-friendly descriptions.
var toolDescs = map[string]string{
	"helix":              "Terminal text editor with modal editing and built-in LSP",
	"neovim":             "Highly extensible terminal text editor",
	"msedit":             "Quick file editing from the terminal via MS Edit",
	"starship":           "Minimal, fast, customizable shell prompt",
	"lsd":                "Modern ls replacement with colors and icons",
	"jq":                 "Lightweight command-line JSON processor",
	"mq":                 "jq for Markdown — query and transform .md files",
	"ripgrep-all":        "ripgrep across PDFs, archives, and more",
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
	entry     brew.BrewfileEntry
	desc      string
	installed bool
}

// Title implements list.DefaultItem. Installed tools get a green check suffix
// so the list mirrors `sci tools` (every recc visible, status at a glance).
func (i reccsItem) Title() string {
	if i.installed {
		return i.entry.Name + " " + uikit.SymOK
	}
	return i.entry.Name
}

// Description implements list.DefaultItem.
func (i reccsItem) Description() string { return i.desc }

// FilterValue implements list.DefaultItem.
func (i reccsItem) FilterValue() string { return i.entry.Name + " " + i.desc }

// reccsModel is the Bubble Tea model for the recommendations list.
type reccsModel struct {
	list     uikit.ListPicker
	entries  []brew.BrewfileEntry
	chosen   int // index into entries; -1 = nothing selected
	quitting bool
}

func newReccsModel(entries []brew.BrewfileEntry, missing map[string]bool) reccsModel {
	items := lo.Map(entries, func(e brew.BrewfileEntry, _ int) reccsItem {
		desc := toolDescs[e.Name]
		if desc == "" {
			desc = e.Type + " package"
		}
		desc += uikit.TUI.TextPink().Render("  " + e.Type)
		return reccsItem{entry: e, desc: desc, installed: !missing[e.Name]}
	})

	installedCount := lo.CountBy(items, func(i reccsItem) bool { return i.installed })
	title := fmt.Sprintf("Recommended tools — %d total, %d installed", len(items), installedCount)
	hints := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "install")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
	return reccsModel{
		list:    uikit.NewListPicker(title, uikit.Items(items), hints...),
		entries: entries,
		chosen:  -1,
	}
}

// Init implements tea.Model.
func (m reccsModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m reccsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.list.IsFiltering() {
			break
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			item, ok := m.list.SelectedItem().(reccsItem)
			if !ok {
				return m, tea.Quit
			}
			if item.installed {
				// Stay in the TUI; flash a transient status so the user can pick
				// another row instead of getting kicked back to the shell.
				m.list.StatusMessage(uikit.TUI.Warn().Render(
					fmt.Sprintf("%s is already installed", item.entry.Name)))
				return m, nil
			}
			_, idx, _ := lo.FindIndexOf(m.entries, func(e brew.BrewfileEntry) bool {
				return e.Name == item.entry.Name
			})
			m.chosen = idx
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

// View implements tea.Model.
func (m reccsModel) View() tea.View {
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}
