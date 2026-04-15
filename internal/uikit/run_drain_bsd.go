//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package uikit

import (
	"os"

	"golang.org/x/sys/unix"
)

// DrainStdin flushes any bytes pending in the stdin buffer. This absorbs
// stale terminal responses (e.g. DECRQM replies for modes 2026/2027) left
// over after a bubbletea program exits. Without this, the responses leak
// into the shell prompt.
//
// Call this after every tea.Program.Run() that writes to a TTY.
func DrainStdin() {
	_ = unix.IoctlSetPointerInt(int(os.Stdin.Fd()), unix.TIOCFLUSH, unix.TCIFLUSH)
}
