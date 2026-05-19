package duck

// subproc_test.go — tests for the close-timeout fallback. These run as
// part of `package duck` (not `duck_test`) so they can drive the subproc
// directly against a non-duckdb child binary, which is the only way to
// reliably simulate a hung child.

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestClose_SigkillsHungChild verifies that close() returns within
// closeTimeout + a small margin even when the child ignores .exit. The
// fallback path SIGKILLs the process group and Wait then returns the
// signal-exit error. We treat any "exited with signal" outcome as success.
func TestClose_SigkillsHungChild(t *testing.T) {
	t.Parallel()

	// `sh -c 'sleep 30'` ignores .exit and won't quit on stdin close, so the
	// only way for close() to return promptly is the SIGKILL fallback. We
	// shrink the per-instance timeout so the test completes in <1 s.
	cmd := exec.Command("sh", "-c", "sleep 30")

	s, err := startSubprocFromCmd(cmd)
	if err != nil {
		t.Fatalf("startSubprocFromCmd: %v", err)
	}
	s.closeTimeout = 200 * time.Millisecond

	start := time.Now()
	closeErr := s.close()
	elapsed := time.Since(start)

	if elapsed > s.closeTimeout+2*time.Second {
		t.Errorf("close() took %v, expected ~%v", elapsed, s.closeTimeout)
	}

	// We expect Wait() to surface a signal-induced exit since SIGKILL got it.
	var exitErr *exec.ExitError
	if !errors.As(closeErr, &exitErr) {
		t.Fatalf("close() = %v, want *exec.ExitError from SIGKILL", closeErr)
	}
	// On Unix the WaitStatus reflects the signal that killed the process.
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
			t.Errorf("WaitStatus = %v, want SIGKILL", ws)
		}
	}
}

// TestClose_PolitePathCompletes verifies the fast/happy path: a child that
// exits when stdin closes (which `cat` does) returns immediately, well
// before the timeout fires.
func TestClose_PolitePathCompletes(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("cat")
	s, err := startSubprocFromCmd(cmd)
	if err != nil {
		t.Fatalf("startSubprocFromCmd: %v", err)
	}

	start := time.Now()
	if err := s.close(); err != nil {
		t.Fatalf("close() = %v, want nil for clean shutdown", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("close() took %v on clean shutdown — should be near-instant", elapsed)
	}
}

// TestClose_Idempotent verifies repeated close() calls are safe.
func TestClose_Idempotent(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("cat")
	s, err := startSubprocFromCmd(cmd)
	if err != nil {
		t.Fatalf("startSubprocFromCmd: %v", err)
	}

	if err := s.close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Second call must not block, panic, or double-Wait.
	done := make(chan error, 1)
	go func() { done <- s.close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("second close: %v, want nil (idempotent)", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second close() blocked — not idempotent")
	}
}
