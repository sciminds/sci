// Package cloudbrowse is the hierarchical bucket browser mounted under
// `sci cloud browse`. The implementation lives in [app]; this root pkg
// only exports the launcher and the interrupt sentinel.
package cloudbrowse

import (
	"errors"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/tui/cloudbrowse/app"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// ErrInterrupted signals the user interrupted the TUI (Ctrl-C). The
// CLI maps this to exit code 130.
var ErrInterrupted = errors.New("interrupted")

// Run launches the cloud bucket browser over the given listing.
// Caller is responsible for fetching `objects` (e.g. via
// share.FetchObjects) so the spinner happens before the alt-screen
// takes over.
func Run(objects []cloud.ObjectInfo, client *cloud.Client) error {
	provider := app.NewProvider(objects, client)
	m := newModel(provider)
	if err := uikit.Run(m); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}

// model wraps browser.Model so we can attach AltScreen via View(). The
// browser package itself stays View()->string (no tea.View) so embedded
// users compose it freely.
type model struct {
	inner browser.Model
}

func newModel(p *app.Provider) model {
	return model{
		inner: browser.New(browser.Config{
			Title:    "cloud",
			Provider: p,
			Actions:  app.BuildActions(p),
			QuitKeys: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		}),
	}
}

func (m model) Init() tea.Cmd { return m.inner.Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	v := tea.NewView(m.inner.View())
	v.AltScreen = true
	return v
}
