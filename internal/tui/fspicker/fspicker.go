// Package fspicker is a local filesystem picker built on uikit/browser.
// Used by `sci cloud put` (and future verbs) when the user omits the
// file argument: pops up an alt-screen TUI starting at cwd, returns the
// absolute path the user selected, or [ErrCancelled] on esc / q / ^C.
//
// Hidden files (names beginning with '.') are filtered by default and
// toggled with the "." key. The picker is single-select only.
package fspicker

import (
	"errors"
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sciminds/cli/internal/tui/fspicker/app"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// ErrCancelled signals the user quit the picker without picking
// (esc, q, ctrl+c, or no entry selected at quit).
var ErrCancelled = errors.New("cancelled")

// Opts configures the picker.
type Opts struct {
	// Start is the directory to open initially. Empty falls back to
	// os.Getwd(). Resolved to an absolute path before display.
	Start string

	// Filter, if set, hides entries for which it returns false.
	// Designed for future use (e.g. only-show-.csv); currently no
	// caller supplies one. Hidden-file filtering is independent and
	// handled by the toggle action.
	Filter func(os.DirEntry) bool
}

// Result is what Pick returns on a successful selection. Path is the
// absolute path of the chosen file or directory. Force is true when
// the user pressed `U` (force-upload) — callers use it to skip the
// remote overwrite check.
type Result struct {
	Path  string
	Force bool
}

// Pick launches the picker and returns the user's selection. Returns
// [ErrCancelled] if they quit without picking.
func Pick(opts Opts) (Result, error) {
	start := opts.Start
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{}, err
		}
		start = cwd
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return Result{}, err
	}

	state := &app.State{}
	provider := app.NewProvider(abs, opts.Filter, state)
	m := newModel(provider, state)
	final, err := uikit.RunModel(m)
	if err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return Result{}, ErrCancelled
		}
		return Result{}, err
	}
	if final.Result().Path == "" {
		return Result{}, ErrCancelled
	}
	return final.Result(), nil
}

// model wraps browser.Model so we can attach AltScreen via View() and
// expose the picked path post-exit. Mirrors the cloudbrowse shape.
type model struct {
	inner browser.Model
	state *app.State
}

func newModel(p *app.Provider, state *app.State) model {
	return model{
		inner: browser.New(browser.Config{
			Title:    "pick file",
			Provider: p,
			Actions:  app.BuildActions(state),
			QuitKeys: key.NewBinding(
				key.WithKeys("q", "esc", "ctrl+c"),
				key.WithHelp("esc", "cancel"),
			),
		}),
		state: state,
	}
}

// Result returns the user's selection. Path is "" when they cancelled.
// Read after [uikit.RunModel] returns.
func (m model) Result() Result {
	return Result{Path: m.state.Picked, Force: m.state.Force}
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd { return m.inner.Init() }

// Update implements tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m model) View() tea.View {
	v := tea.NewView(m.inner.View())
	v.AltScreen = true
	return v
}
