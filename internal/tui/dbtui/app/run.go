package app

// run.go — entry point [Run] that launches the Bubble Tea program with a
// [DataStore], configures the terminal, and returns on quit or interrupt.

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
)

// ErrInterrupted signals that the user interrupted the TUI (e.g. Ctrl-C).
// Callers should exit with code 130.
var ErrInterrupted = errors.New("interrupted")

// RunOption configures optional viewer behaviour.
type RunOption func(*runConfig)

type runConfig struct {
	colHints   map[string]ColHint
	initialTab string
}

// WithInitialTab makes the viewer open on the named tab (if it exists).
func WithInitialTab(name string) RunOption {
	return func(c *runConfig) { c.initialTab = name }
}

// WithColHints sets column width hints for the viewer.
func WithColHints(hints map[string]ColHint) RunOption {
	return func(c *runConfig) { c.colHints = hints }
}

// Run launches the interactive database viewer with a pre-opened store.
// The caller is responsible for closing the store.
func Run(store data.DataStore, label string, opts ...RunOption) error {
	var cfg runConfig
	for _, o := range opts {
		o(&cfg)
	}

	model, err := NewModel(store, label, false)
	if err != nil {
		return err
	}
	if cfg.colHints != nil {
		model.ApplyColHints(cfg.colHints)
	}
	if cfg.initialTab != "" {
		model.SelectTab(cfg.initialTab)
	}

	fmt.Fprintf(os.Stderr, "\033[22;2t\033]2;%s\007", "sci")
	defer fmt.Fprint(os.Stderr, "\033[23;2t")

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
