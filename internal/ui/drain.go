package ui

import (
	"os"

	"golang.org/x/sys/unix"
)

// drainStdin flushes any bytes pending in the stdin buffer. This absorbs
// stale terminal responses (e.g. DECRQM replies for modes 2026/2027) left
// over after a bubbletea program exits. Without this, the responses leak
// into the shell prompt.
func drainStdin() {
	fd := int(os.Stdin.Fd())
	what := unix.TCIFLUSH // flush queued input only
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, what)
}
