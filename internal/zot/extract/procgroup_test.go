package extract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestConfigureProcessGroup_KillsGrandchild spawns a shell that
// backgrounds a sleeper grandchild, then cancels ctx. Both the shell
// (direct child) and the sleeper (grandchild) must die — proving the
// signal reached the whole process group, not just the direct child.
//
// Without configureProcessGroup, the grandchild inherits our process
// group and survives SIGKILL-to-the-direct-child; that's the exact
// path docling's Python workers take, which is how users end up with
// zombie torch/OCR processes after ctrl-c.
func TestConfigureProcessGroup_KillsGrandchild(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "grandchild.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Shell forks a sleeper, records its pid, waits forever. `exec`
	// on the wait makes the shell the group leader for both itself
	// and the sleeper.
	script := "sleep 60 & echo $! > " + pidFile + "; wait"
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	configureProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	gcPID := waitForPID(t, pidFile, 3*time.Second)
	if gcPID == 0 {
		_ = cmd.Process.Kill()
		t.Fatal("grandchild PID was never written")
	}

	// Sanity check: grandchild is alive before cancel.
	if err := syscall.Kill(gcPID, 0); err != nil {
		t.Fatalf("expected grandchild %d alive pre-cancel, got: %v", gcPID, err)
	}

	// Cancel — Go invokes cmd.Cancel, which must SIGKILL the group.
	cancel()
	_ = cmd.Wait() // err is expected (killed)

	// init/launchd reaps the orphaned grandchild near-instantly once
	// it dies. Poll until Kill(pid, 0) returns ESRCH.
	if waitForPIDGone(gcPID, 2*time.Second) {
		return
	}
	_ = syscall.Kill(gcPID, syscall.SIGKILL) // cleanup
	t.Fatalf("grandchild %d still alive 2s after cancel — group signal did not reach it", gcPID)
}

// waitForPID polls pidFile until it contains a valid PID or timeout
// expires. Returns 0 on timeout.
func waitForPID(t *testing.T, pidFile string, timeout time.Duration) int {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, err := os.ReadFile(pidFile); err == nil && len(b) > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
				return n
			}
		}
		select {
		case <-deadline:
			return 0
		case <-tick.C:
		}
	}
}

// waitForPIDGone returns true once Kill(pid, 0) reports ESRCH,
// meaning the kernel has reaped the process.
func waitForPIDGone(pid int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-tick.C:
		}
	}
}
