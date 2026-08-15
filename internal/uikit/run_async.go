package uikit

import (
	"runtime/debug"

	tea "charm.land/bubbletea/v2"
)

// CommandPanicMsg is emitted by [SafeCmd] when the wrapped function panics.
// A panic in a tea.Cmd runs in its own goroutine, which Bubble Tea does not
// recover — left unhandled it crashes the process with the terminal still in
// raw mode (no cursor, no echo). Converting it to a message lets the runtime
// ([Run] / [RunModel]) quit cleanly, restore the terminal, and surface the
// panic as [ErrCommandPanic].
// Models normally never receive this message; the runtime intercepts it.
type CommandPanicMsg struct {
	Value any    // the recovered panic value
	Stack []byte // stack trace captured at recover time
}

// SafeCmd wraps a command closure so a panic inside it becomes a
// [CommandPanicMsg] instead of crashing the goroutine and wedging the
// terminal. Every tea.Cmd in this repo goes through it; the
// rules/recover-command-panic.yml lint rule rejects a raw func() tea.Msg.
//
// There is deliberately no generic (T, error) variant. One was tried and
// removed: it necessarily returns a Result[T] message, which erases the
// caller's own named message type, and a TUI command almost always has to
// correlate its result back to the state that requested it — a tab index, a
// request sequence number, the path that was listed. Those fields have
// nowhere to live in a Result[T], and collapsing two commands onto the same
// payload type makes them indistinguishable in a model's type switch. Return
// your own named message struct and carry whatever correlation data it needs.
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
