package uikit

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Result is a generic outcome from an async command. Use in a type switch
// to discriminate by payload type:
//
//	case kit.Result[[]Board]:
//	    if msg.Err != nil { … }
//	    m.boards = msg.Value
type Result[T any] struct {
	Value T
	Err   error
}

// AsyncCmd wraps a fallible function into a tea.Cmd that returns
// [Result][T]. The function runs synchronously inside the Cmd goroutine
// (Bubbletea already runs Cmds off the main loop).
func AsyncCmd[T any](fn func() (T, error)) tea.Cmd {
	return func() tea.Msg {
		val, err := fn()
		return Result[T]{Value: val, Err: err}
	}
}

// AsyncCmdCtx wraps a context-aware function with a timeout into a
// tea.Cmd. The derived context is canceled after the function returns or
// when timeout elapses, whichever comes first.
func AsyncCmdCtx[T any](ctx context.Context, timeout time.Duration, fn func(context.Context) (T, error)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		val, err := fn(ctx)
		return Result[T]{Value: val, Err: err}
	}
}
