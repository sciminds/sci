//go:build linux

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
// Linux exposes the same kernel operation as the BSD TIOCFLUSH ioctl via
// TCFLSH, taking an int argument instead of a pointer.
func DrainStdin() {
	_ = unix.IoctlSetInt(int(os.Stdin.Fd()), unix.TCFLSH, int(unix.TCIFLUSH))
}
