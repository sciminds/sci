package brew

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCheckUpdates_ReturnsWhenContextExpires is the regression test for the
// hang: `brew update` can block indefinitely (a git fetch against a captive
// portal never gets an answer), and nothing above it had a deadline. The mock
// runner's Update blocks forever; CheckUpdates must still return.
//
// The context is cancelled up front rather than given a wall-clock deadline so
// the test proves the mechanism without depending on timing.
func TestCheckUpdates_ReturnsWhenContextExpires(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	mock := &mockRunner{updateBlock: block}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	check, err := CheckUpdates(ctx, mock, nil)
	if !errors.Is(err, ErrCheckTimeout) {
		t.Fatalf("CheckUpdates err = %v, want ErrCheckTimeout", err)
	}
	if len(check.All()) != 0 {
		t.Errorf("expected no packages on timeout, got %v", check.All())
	}
}

// TestCheckUpdates_DeadlineElapses covers the real-clock path: a deadline that
// expires while the runner is still working.
func TestCheckUpdates_DeadlineElapses(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	mock := &mockRunner{updateBlock: block}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if _, err := CheckUpdates(ctx, mock, nil); !errors.Is(err, ErrCheckTimeout) {
		t.Fatalf("CheckUpdates err = %v, want ErrCheckTimeout", err)
	}
}

// TestCheckUpdates_Success returns both package lists when the runner answers
// in time, and never upgrades anything — this call is a read.
func TestCheckUpdates_Success(t *testing.T) {
	mock := &mockRunner{
		outdatedResult:   []OutdatedPackage{{Type: TypeFormula, Name: "git", InstalledVersion: "2.44.0", CurrentVersion: "2.45.0"}},
		uvOutdatedResult: []OutdatedPackage{{Type: TypeUV, Name: "marimo", InstalledVersion: "0.22.4", CurrentVersion: "0.23.0"}},
	}

	check, err := CheckUpdates(t.Context(), mock, nil)
	if err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	if len(check.Brew) != 1 || len(check.UV) != 1 {
		t.Fatalf("check = %+v, want one brew and one uv package", check)
	}
	if len(check.All()) != 2 {
		t.Errorf("All() = %v, want 2 packages", check.All())
	}
	if mock.updateCalls != 1 {
		t.Errorf("updateCalls = %d, want 1", mock.updateCalls)
	}
	if mock.upgradeCalls != 0 || mock.uvUpgradeCalls != 0 {
		t.Error("CheckUpdates must not upgrade anything")
	}
}

// TestCheckUpdates_UpdateError surfaces a `brew update` failure as an ordinary
// error, not a timeout.
func TestCheckUpdates_UpdateError(t *testing.T) {
	mock := &mockRunner{updateErr: errors.New("no such command")}

	_, err := CheckUpdates(t.Context(), mock, nil)
	if err == nil {
		t.Fatal("expected an error when brew update fails")
	}
	if errors.Is(err, ErrCheckTimeout) {
		t.Errorf("a runner failure must not be reported as a timeout: %v", err)
	}
}
