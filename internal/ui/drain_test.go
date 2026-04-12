package ui

import (
	"testing"
)

// Why DrainStdin exists (charmbracelet/bubbletea#1590):
//
// Bubbletea v2 sends DECRQM queries for synchronized output (mode 2026) and
// unicode core (mode 2027) during startup. These are written to the output
// (stderr in our case), and the terminal responds asynchronously on stdin.
//
// For short-lived inline programs (spinners, progress bars), bubbletea often
// exits before the terminal responds. The response bytes then leak into the
// shell prompt as garbage like: ^[[?2026;2$y^[[?2027;0$y
//
// DrainStdin calls TIOCFLUSH (kernel-level ioctl) to discard any queued input
// immediately after p.Run() returns. This is called in RunWithSpinner,
// RunWithSpinnerStatus, and RunWithProgress.
//
// The ioctl silently no-ops on non-TTY fds (pipes in CI / go test), so there
// is no risk of test failures or panics in non-interactive environments.

func TestDrainStdin_SafeOnNonTTY(t *testing.T) {
	t.Parallel()
	// In CI and `go test`, stdin is a pipe, not a TTY.
	// DrainStdin must not panic — the TIOCFLUSH ioctl silently fails
	// on non-TTY fds (returns ENOTTY, which we ignore).
	DrainStdin()
}

func TestDrainStdin_Idempotent(t *testing.T) {
	t.Parallel()
	// Multiple calls in succession must be safe. This can happen when
	// RunWithSpinner is followed immediately by RunWithProgress (as in
	// extract-lib: plan spinner → progress bar).
	DrainStdin()
	DrainStdin()
	DrainStdin()
}
