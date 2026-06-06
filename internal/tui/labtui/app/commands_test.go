package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/lab"
)

// TestTransferWithRecoverConvertsPanicToError verifies that a panic inside
// Backend.Transfer becomes an error rather than crashing the process. The
// transfer goroutine spawned by startTransferCmd runs outside uikit.SafeCmd's
// guard, so it needs its own recovery to avoid wedging the terminal.
func TestTransferWithRecoverConvertsPanicToError(t *testing.T) {
	fb := newFakeBackend()
	fb.transferPanic = true

	ch := make(chan lab.Progress, 1)
	err := transferWithRecover(context.Background(), fb, "remote/x", t.TempDir(), ch)
	if err == nil {
		t.Fatal("expected an error from a panicking transfer, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error %q should mention the panic", err)
	}
}
