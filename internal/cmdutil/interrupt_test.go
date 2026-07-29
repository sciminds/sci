package cmdutil

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Not parallel: the test sends a real SIGHUP to the whole test process
// and relies on InterruptContext's handler being the one that catches it.
func TestInterruptContext_SignalCancels(t *testing.T) {
	ctx, stop := InterruptContext(context.Background())
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("context not canceled after SIGHUP")
	}
}

func TestInterruptContext_ParentCancelPropagates(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := InterruptContext(parent)
	defer stop()

	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("parent cancellation did not propagate")
	}
}

func TestInterruptContext_StopCancels(t *testing.T) {
	t.Parallel()
	ctx, stop := InterruptContext(context.Background())
	stop()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stop should cancel the context")
	}
}

// watchEOF is exercised synchronously — no goroutines, no sleeps. A zero
// grace means every EOF counts as "after the grace window"; a huge grace
// means every EOF counts as "immediate" and must disarm.

func TestWatchEOF_EOFAfterGraceCancels(t *testing.T) {
	t.Parallel()
	called := false
	watchEOF(strings.NewReader(""), 0, func() { called = true })
	if !called {
		t.Fatal("EOF past the grace window must cancel")
	}
}

func TestWatchEOF_ImmediateEOFDisarms(t *testing.T) {
	t.Parallel()
	called := false
	watchEOF(strings.NewReader("stray buffered input"), time.Hour, func() { called = true })
	if called {
		t.Fatal("EOF within the grace window must not cancel — stdin was empty from the start")
	}
}
