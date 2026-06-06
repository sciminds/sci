package uikit

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ErrCommandPanic is returned by [Run] and [RunModel] when a command wrapped
// by [SafeCmd] / [AsyncCmd] / [AsyncCmdCtx] panicked. The terminal is restored
// (the program quits cleanly) before it surfaces, and the wrapped error string
// includes the panic value and stack trace. Test for it with
// errors.Is(err, ErrCommandPanic).
var ErrCommandPanic = errors.New("command panicked")

// panicGuard is a transparent wrapper model that forwards every message to
// the inner model except [CommandPanicMsg], which it captures and turns into
// a clean tea.Quit. After the program exits, [Run] / [RunModel] inspect the
// captured panic so the terminal is restored before the panic is reported.
type panicGuard struct {
	inner    tea.Model
	captured *CommandPanicMsg
}

// Init forwards to the inner model.
func (g panicGuard) Init() tea.Cmd { return g.inner.Init() }

// Update captures a [CommandPanicMsg] (quitting cleanly so the terminal is
// restored) and forwards every other message to the inner model.
func (g panicGuard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cp, ok := msg.(CommandPanicMsg); ok {
		g.captured = &cp
		return g, tea.Quit
	}
	var cmd tea.Cmd
	g.inner, cmd = g.inner.Update(msg)
	return g, cmd
}

// View forwards to the inner model.
func (g panicGuard) View() tea.View { return g.inner.View() }

// reportCommandPanic formats a captured command panic into an error that
// wraps [ErrCommandPanic] and carries the panic value and stack trace.
func reportCommandPanic(cp CommandPanicMsg) error {
	return fmt.Errorf("%w: %v\n\n%s", ErrCommandPanic, cp.Value, cp.Stack)
}

// Run launches a Bubbletea program and returns its error. It drains
// stdin after the program exits so leftover bytes don't leak into the
// shell. Use this for TUIs that don't need the final model state.
//
// A panic in a command (see [SafeCmd]) is caught, the terminal restored,
// and the panic returned as [ErrCommandPanic] rather than left to wedge the
// terminal.
//
// Optional [tea.ProgramOption] values are forwarded to [tea.NewProgram]
// (e.g. tea.WithOutput(os.Stderr) for inline spinners).
//
//	if err := kit.Run(myModel); err != nil { … }
func Run(m tea.Model, opts ...tea.ProgramOption) error {
	p := tea.NewProgram(panicGuard{inner: m}, opts...)
	final, err := p.Run()
	DrainStdin()
	if err != nil {
		return err
	}
	if g, ok := final.(panicGuard); ok && g.captured != nil {
		return reportCommandPanic(*g.captured)
	}
	return nil
}

// RunModel launches a Bubbletea program and returns the final model
// cast back to its concrete type. Use this when you need to inspect
// model state after the TUI exits (e.g. to read the user's selection).
//
// A command panic is handled exactly as in [Run]: the terminal is restored
// and [ErrCommandPanic] is returned (with the zero value of M).
//
//	final, err := kit.RunModel(myModel)
//	if err != nil { … }
//	fmt.Println(final.Chosen)
func RunModel[M tea.Model](m M, opts ...tea.ProgramOption) (M, error) {
	p := tea.NewProgram(panicGuard{inner: m}, opts...)
	final, err := p.Run()
	DrainStdin()
	if err != nil {
		var zero M
		return zero, err
	}
	g := final.(panicGuard)
	if g.captured != nil {
		var zero M
		return zero, reportCommandPanic(*g.captured)
	}
	return g.inner.(M), nil
}
