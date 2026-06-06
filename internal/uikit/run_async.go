package uikit

import (
	"context"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Result is a generic outcome from an async command. Use in a type switch
// to discriminate by payload type:
//
//	case kit.Result[[]ObjectInfo]:
//	    if msg.Err != nil { … }
//	    m.objects = msg.Value
type Result[T any] struct {
	Value T
	Err   error
}

// CommandPanicMsg is emitted by [SafeCmd], [AsyncCmd], and [AsyncCmdCtx]
// when the wrapped function panics. A panic in a tea.Cmd runs in its own
// goroutine, which Bubble Tea does not recover — left unhandled it crashes
// the process with the terminal still in raw mode (no cursor, no echo).
// Converting it to a message lets the runtime ([Run] / [RunModel]) quit
// cleanly, restore the terminal, and surface the panic as [ErrCommandPanic].
// Models normally never receive this message; the runtime intercepts it.
type CommandPanicMsg struct {
	Value any    // the recovered panic value
	Stack []byte // stack trace captured at recover time
}

// SafeCmd wraps a command closure so a panic inside it becomes a
// [CommandPanicMsg] instead of crashing the goroutine and wedging the
// terminal. Use it for raw func() tea.Msg commands that don't fit
// [AsyncCmd]'s (T, error) shape; [AsyncCmd] and [AsyncCmdCtx] already apply
// the same guard internally.
func SafeCmd(fn func() tea.Msg) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = CommandPanicMsg{Value: r, Stack: debug.Stack()}
			}
		}()
		return fn()
	}
}

// AsyncCmd wraps a fallible function into a tea.Cmd that returns
// [Result][T]. The function runs synchronously inside the Cmd goroutine
// (Bubbletea already runs Cmds off the main loop). A panic in fn is
// recovered and surfaced as a [CommandPanicMsg] rather than crashing the
// process.
func AsyncCmd[T any](fn func() (T, error)) tea.Cmd {
	return SafeCmd(func() tea.Msg {
		val, err := fn()
		return Result[T]{Value: val, Err: err}
	})
}

// AsyncCmdCtx wraps a context-aware function with a timeout into a
// tea.Cmd. The derived context is canceled after the function returns or
// when timeout elapses, whichever comes first. A panic in fn is recovered
// and surfaced as a [CommandPanicMsg].
func AsyncCmdCtx[T any](ctx context.Context, timeout time.Duration, fn func(context.Context) (T, error)) tea.Cmd {
	return SafeCmd(func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		val, err := fn(ctx)
		return Result[T]{Value: val, Err: err}
	})
}
