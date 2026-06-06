package uikit

// run_debug.go — opt-in message-stream dump for [Run] / [RunModel]. When the
// SCI_TUI_DEBUG env var names a file, every tea.Msg that reaches the program is
// pretty-printed to it, giving a `tail -f`-able trace of the Elm loop. This is
// the fastest way to debug a TUI whose state is routed by many message types
// (e.g. dbtui's overlay stack). Off by default: when the var is unset the
// dumper is nil and every hook is a zero-cost no-op.

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/davecgh/go-spew/spew"
)

// TUIDebugEnv is the environment variable that turns on the message dump: set it
// to a writable file path and [Run] / [RunModel] append a pretty-printed entry
// for every tea.Msg. Unset (or empty), or in quiet/--json mode, nothing is
// written and there is no overhead.
//
//	SCI_TUI_DEBUG=/tmp/sci-tui.log sci view data.db   # then: tail -f /tmp/sci-tui.log
const TUIDebugEnv = "SCI_TUI_DEBUG"

// spewConfig formats messages deterministically: no pointer addresses or
// capacities (stable across runs and tail-friendly), methods disabled so a
// String()/Error() on a message can't blow up or recurse, and a depth cap so a
// message carrying a whole sub-model doesn't flood the log.
var spewConfig = &spew.ConfigState{
	Indent:                  "  ",
	DisableMethods:          true,
	DisablePointerAddresses: true,
	DisableCapacities:       true,
	SortKeys:                true,
	MaxDepth:                6,
}

// msgDumper writes a pretty-printed record of each tea.Msg to a file. A nil
// *msgDumper is the off state — every method is a safe no-op, so callers never
// branch on whether debugging is enabled.
type msgDumper struct {
	w *os.File
	n int
}

// newMsgDumper returns a dumper writing to the file named by [TUIDebugEnv], or
// nil when the var is unset/empty, quiet (--json) mode is active, or the file
// can't be opened. The file is truncated so each run starts with a clean trace.
func newMsgDumper() *msgDumper {
	if quiet {
		return nil
	}
	path := strings.TrimSpace(os.Getenv(TUIDebugEnv))
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		// A debug aid must never break the TUI: fail silent and stay off.
		return nil
	}
	return &msgDumper{w: f}
}

// log appends one entry (sequence number, wall-clock time, concrete type, and a
// spew dump of the value) for msg. No-op on a nil dumper. Safe to call without a
// lock: Bubble Tea delivers messages to Update serially on one goroutine.
func (d *msgDumper) log(msg tea.Msg) {
	if d == nil {
		return
	}
	d.n++
	// Best-effort: a failed debug write must never disrupt the TUI.
	_, _ = fmt.Fprintf(d.w, "── #%d  %s  %T ──\n%s\n", d.n, time.Now().Format("15:04:05.000"), msg, spewConfig.Sdump(msg))
}

// close releases the underlying file. No-op on a nil dumper.
func (d *msgDumper) close() {
	if d == nil {
		return
	}
	_ = d.w.Close()
}
