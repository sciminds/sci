package brew

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
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
		desc = p.Desc + uikit.TUI.TextPink().Render("  "+p.Version)
	case p.Desc != "":
		desc = p.Desc
	case p.Version != "":
		desc = uikit.TUI.TextPink().Render(p.Version)
	default:
		desc = uikit.TUI.Dim().Render("no description")
	}

	return listItem{
		title:  title,
		desc:   desc,
		filter: p.Name + " " + p.Desc,
	}
}

// listModel is the Bubble Tea model for the interactive package list.
type listModel struct {
	list uikit.ListPicker
}

func newListModel(packages []PackageInfo) listModel {
	items := uikit.Items(lo.Map(packages, func(p PackageInfo, _ int) listItem {
		return makeListItem(p)
	}))

	title := fmt.Sprintf("Brewfile — %d packages", len(packages))
	qHint := key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	return listModel{list: uikit.NewListPicker(title, items, qHint)}
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
		if m.list.IsFiltering() {
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
	if err := uikit.Run(newListModel(packages)); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
