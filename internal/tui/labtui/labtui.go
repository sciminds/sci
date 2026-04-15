// Package labtui is the interactive lab storage browser. It wraps the
// app package model with a Run entry point that constructs an SSH-backed
// Backend, launches the Bubbletea program via uikit.Run, and translates
// tea.ErrInterrupted into the package's ErrInterrupted sentinel.
package labtui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/tui/labtui/app"
	"github.com/sciminds/cli/internal/uikit"
)

// ErrInterrupted is returned when the user quits via Ctrl-C; CLI callers
// should exit with code 130.
var ErrInterrupted = errors.New("interrupted")

// Run launches the lab browser TUI for the configured user.
func Run(cfg *lab.Config) error {
	backend := app.NewSSHBackend(cfg)
	model := app.NewModel(cfg, backend)
	if err := uikit.Run(model); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
