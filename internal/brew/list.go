package brew

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/kit"
	"github.com/sciminds/cli/internal/ui"
)

// ErrInterrupted signals the user interrupted the TUI (Ctrl-C).
// Callers should exit with code 130.
var ErrInterrupted = errors.New("interrupted")

// listItem implements list.Item for the bubbles list component.
type listItem struct {
	title, desc, filter string
}

// Title implements list.DefaultItem.
func (i listItem) Title() string { return i.title }

// Description implements list.DefaultItem.
func (i listItem) Description() string { return i.desc }

// FilterValue implements list.DefaultItem.
func (i listItem) FilterValue() string { return i.filter }

func makeListItem(p PackageInfo) listItem {
	title := p.Name
	if p.Type == "cask" {
		title += " (cask)"
	}

	var desc string
	switch {
	case p.Desc != "" && p.Version != "":
		desc = p.Desc + ui.TUI.Muted().Render("  "+p.Version)
	case p.Desc != "":
		desc = p.Desc
	case p.Version != "":
		desc = ui.TUI.Muted().Render(p.Version)
	default:
		desc = ui.TUI.Dim().Render("no description")
	}

	return listItem{
		title:  title,
		desc:   desc,
		filter: p.Name + " " + p.Desc,
	}
}

// listModel is the Bubble Tea model for the interactive package list.
type listModel struct {
	list list.Model
}

func newListModel(packages []PackageInfo) listModel {
	items := lo.Map(packages, func(p PackageInfo, _ int) list.Item {
		return makeListItem(p)
	})

	title := fmt.Sprintf("Brewfile — %d packages", len(packages))
	delegate := ui.NewListDelegate()
	l := list.New(items, delegate, 0, 0)
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return listModel{list: l}
}

// Init implements tea.Model.
func (m listModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Don't intercept keys while filtering.
		if m.list.FilterState() == list.Filtering {
			break
		}
		if msg.String() == "q" || msg.String() == "ctrl+c" {
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
func (m listModel) View() tea.View {
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}

// RunListTUI launches the interactive package list.
func RunListTUI(packages []PackageInfo) error {
	if err := kit.Run(newListModel(packages)); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
