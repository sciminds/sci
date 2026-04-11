package board

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/app"
)

// ErrInterrupted signals that the user interrupted the TUI (e.g. Ctrl-C).
// Callers should exit with code 130 (matches dbtui).
var ErrInterrupted = errors.New("interrupted")

// Run launches the interactive kanban TUI on an already-constructed Store.
// The caller owns the Store's lifetime (closing the underlying LocalCache).
//
// If initialBoard is non-empty the TUI opens directly into that board's grid;
// otherwise it starts on the board picker.
func Run(store *engine.Store, initialBoard string) error {
	m := app.NewModel(store, initialBoard)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
