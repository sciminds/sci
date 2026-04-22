package extract

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProcessGroup makes cmd the leader of a new process group
// and wires ctx cancellation to SIGKILL the entire group — not just
// the direct child.
//
// Why this matters for docling: docling is Python, and it spawns its
// own worker processes (torch, OCR, layout models). With the default
// exec.CommandContext behavior, canceling ctx sends SIGKILL only to
// docling itself; the Python grandchildren get reparented to
// init/launchd and keep consuming CPU/GPU until they finish on their
// own. `Setpgid: true` puts docling in its own group, and the Cancel
// hook signals the whole group (negative PID) so every descendant
// dies with it.
//
// Unix-only (darwin + linux). The project doesn't build on Windows.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Negative PID targets the process group (leader + descendants).
		// Errors here are best-effort — ESRCH just means the group
		// already exited. Returning os.ErrProcessDone tells Wait we
		// handled cancellation cleanly.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return os.ErrProcessDone
	}
	// Safety net: if Cancel somehow leaves the process alive, Go's
	// Wait goroutine SIGKILLs the direct child after this delay.
	cmd.WaitDelay = 5 * time.Second
}
