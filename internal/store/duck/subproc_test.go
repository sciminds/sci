package duck

// subproc_test.go — tests for the close-timeout fallback. These run as
// part of `package duck` (not `duck_test`) so they can drive the subproc
// directly against a non-duckdb child binary, which is the only way to
// reliably simulate a hung child.

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStartSubprocRejectsDashPath confirms a path starting with `-` is
// rejected before any subprocess is spawned. Without this guard, a path
// like `-c "ATTACH '/etc/passwd'"` would be parsed by duckdb as a flag.
func TestStartSubprocRejectsDashPath(t *testing.T) {
	t.Parallel()
	_, err := startSubproc("-malicious.duckdb")
	if err == nil {
		t.Fatal("startSubproc(-malicious.duckdb) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error %q does not mention refusal", err)
	}
}

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

// TestDrainStderr_TrailerDoesNotLeakAcrossQueries pins the fix for the
// race where Q_N's synthetic catalog-error trailer (the 4 lines after
// "Catalog Error: ...": hint, blank separator, "LINE 1:" echo, caret)
// leaked into Q_{N+1}'s stderrBuf when Q_{N+1} armed before drainStderr
// had a chance to read those trailing lines. Symptom: spurious errors
// like `duckdb: Did you mean "duckdb_constraints"? LINE 1: SELECT 1 FROM
// __sci_duck_stderr_sentinel__N__;` or a lone `duckdb: ^` flaking
// TestStoreContract under -race.
//
// The test simulates the race deterministically by re-arming the sentinel
// AFTER drainStderr signals it matched line 1 but BEFORE the trailer is
// fed into the pipe. io.Pipe is unbuffered, so drainStderr blocks on Read
// between the match and the next write — that's our synchronisation point.
func TestDrainStderr_TrailerDoesNotLeakAcrossQueries(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	s := &subproc{stderrDie: make(chan struct{})}
	go s.drainStderr(pr)
	defer func() {
		_ = pw.Close() // signals io.EOF; drainStderr exits, closes stderrDie
		<-s.stderrDie
	}()

	arm := func(marker string) chan struct{} {
		seen := make(chan struct{})
		s.stderrMu.Lock()
		s.stderrBuf.Reset()
		s.stderrSentinel = marker
		s.stderrSeen = seen
		s.stderrMu.Unlock()
		return seen
	}
	write := func(line string) {
		if _, err := io.WriteString(pw, line+"\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Q1: arm with _1_, write its synthetic line 1, wait for match. After
	// this returns, drainStderr is blocked in scanner.Scan() waiting for
	// the next pipe Read — so re-arming below is race-free.
	seen1 := arm("__sci_duck_stderr_sentinel__1__")
	write(`Catalog Error: Table "__sci_duck_stderr_sentinel__1__" does not exist!`)
	<-seen1
	if got := s.stderrSnapshot(); got != "" {
		t.Errorf("Q1 buf after match = %q; want empty (line 1 should be consumed by sentinel match)", got)
	}

	// Q2 arms BEFORE Q1's trailer is fed in. Without the fix, Q1's 3
	// trailer lines would be appended to Q2's freshly-reset buf because
	// none of them contain Q2's marker.
	seen2 := arm("__sci_duck_stderr_sentinel__2__")

	// Q1 trailer (hint, blank, LINE 1 echo, caret) then Q2 full 5-line
	// synthetic block — mirroring the actual duckdb v1.5.x error format.
	write(`Did you mean "duckdb_constraints"?`)
	write(``)
	write(`LINE 1: SELECT 1 FROM __sci_duck_stderr_sentinel__1__;`)
	write(`                      ^`)
	write(`Catalog Error: Table "__sci_duck_stderr_sentinel__2__" does not exist!`)
	write(`Did you mean "duckdb_constraints"?`)
	write(``)
	write(`LINE 1: SELECT 1 FROM __sci_duck_stderr_sentinel__2__;`)
	write(`                      ^`)

	<-seen2
	if got := s.stderrSnapshot(); got != "" {
		t.Errorf("Q2 buf after match = %q; want empty (Q1 trailer leaked into Q2)", got)
	}
}

// TestDrainStderr_PreservesRealUserErrors confirms the trailer-drop counter
// does not eat legitimate user-error lines that arrive *after* the
// trailing-drop window has closed.
func TestDrainStderr_PreservesRealUserErrors(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	s := &subproc{stderrDie: make(chan struct{})}
	go s.drainStderr(pr)
	defer func() {
		_ = pw.Close()
		<-s.stderrDie
	}()

	arm := func(marker string) chan struct{} {
		seen := make(chan struct{})
		s.stderrMu.Lock()
		s.stderrBuf.Reset()
		s.stderrSentinel = marker
		s.stderrSeen = seen
		s.stderrMu.Unlock()
		return seen
	}
	write := func(line string) {
		if _, err := io.WriteString(pw, line+"\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Q1 with user error: user-error line arrives BEFORE the synthetic
	// sentinel line, so it must be captured.
	seen1 := arm("__sci_duck_stderr_sentinel__1__")
	write(`Parser Error: syntax error at or near "FROMM"`)
	write(`Catalog Error: Table "__sci_duck_stderr_sentinel__1__" does not exist!`)
	<-seen1
	if got := s.stderrSnapshot(); !strings.Contains(got, "Parser Error") {
		t.Errorf("Q1 buf = %q; want it to contain Parser Error", got)
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
