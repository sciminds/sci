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

// Option configures the TUI before it runs. Pass via Run's variadic arg.
type Option func(*app.Model)

// WithInitialGridCursor places the grid cursor on the given column the
// first time the initialBoard loads. No-op if initialBoard is empty.
func WithInitialGridCursor(col int) Option {
	return func(m *app.Model) { m.SetInitialGridCursor(col) }
}

// Run launches the interactive kanban TUI on an already-constructed Store.
// The caller owns the Store's lifetime (closing the underlying LocalCache).
//
// If initialBoard is non-empty the TUI opens directly into that board's grid;
// otherwise it starts on the board picker.
func Run(store *engine.Store, initialBoard string, opts ...Option) error {
	m := app.NewModel(store, initialBoard)
	for _, opt := range opts {
		opt(m)
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
