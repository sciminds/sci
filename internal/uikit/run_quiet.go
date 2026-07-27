package uikit

// quiet.go — global toggle for non-interactive (--json) mode.
// When quiet, spinners are suppressed, progress bars are skipped,
// and the work function runs directly with status printed to stderr.

import (
	"os"

	"golang.org/x/term"
)

var quiet bool

// SetQuiet enables or disables quiet (non-interactive) mode.
// Called from the root command's Before hook when --json is set.
func SetQuiet(q bool) { quiet = q }

// IsQuiet reports whether quiet mode is active.
func IsQuiet() bool { return quiet }

// interactive reports whether the inline runners ([RunWithSpinner],
// [RunWithSpinnerStatus], [RunWithProgress]) can draw. It requires quiet mode
// to be off and os.Stderr — where those runners render — to be a terminal.
//
// The stderr probe is what makes a headless shell (no controlling terminal: a
// CI runner, an agent harness, `ssh host sci …`) take the quiet path instead of
// having bubbletea fail on /dev/tty. Without it the runners returned that TUI
// error while the work goroutine completed the write anyway, so a successful
// --apply reported failure.
//
// It deliberately does not probe stdin: with a real terminal attached,
// bubbletea opens /dev/tty for input, so `sci … < file` keeps its spinner.
func interactive() bool {
	return !IsQuiet() && term.IsTerminal(int(os.Stderr.Fd()))
}
