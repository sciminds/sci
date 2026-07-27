package uikit

import (
	"errors"
	"strings"
	"testing"
)

// These tests exercise the non-TTY fallback: with quiet mode OFF (no --json),
// a destination that isn't a terminal must still run the work and report the
// work's own outcome — never a bubbletea "could not open a new TTY" error for
// work that actually succeeded. captureStderr swaps os.Stderr for a pipe, so
// the non-TTY condition holds even when the test binary is run from a terminal.

func TestRunWithProgress_NonTTYRunsFnAndReportsSuccess(t *testing.T) {
	SetQuiet(false)
	t.Cleanup(func() { SetQuiet(false) })

	ran := false
	var got error
	out := captureStderr(t, func() {
		got = RunWithProgress("Linking notes…", func(tr *ProgressTracker) error {
			tr.SetTotal(2)
			tr.SetTitle("phase 2")
			tr.Advance("created", "ZJQMDFWK")
			tr.Status("halfway")
			tr.Reset("phase 3", 1)
			tr.Advance("created", "JACFHMN6")
			ran = true
			return nil
		})
	})

	if !ran {
		t.Fatal("fn should have been called")
	}
	if got != nil {
		t.Errorf("work succeeded, so the returned error should be nil; got %v", got)
	}
	if !strings.Contains(out, "Linking notes…") {
		t.Errorf("stderr should carry the title, got %q", out)
	}
}

func TestRunWithProgress_NonTTYReturnsFnError(t *testing.T) {
	SetQuiet(false)
	t.Cleanup(func() { SetQuiet(false) })

	want := errors.New("boom")
	var got error
	captureStderr(t, func() {
		got = RunWithProgress("Linking notes…", func(*ProgressTracker) error {
			return want
		})
	})

	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestReportDisplayFailure covers the second layer: a TTY was present, so the
// runners started bubbletea, but the program itself failed. The work goroutine
// is unaffected, so its error — not the display error — must be returned.
func TestReportDisplayFailure_ReturnsWorkErrorNotDisplayError(t *testing.T) {
	work := make(chan error, 1)
	work <- nil // the write succeeded

	var got error
	out := captureStderr(t, func() {
		got = reportDisplayFailure("Linking", errors.New("could not open a new TTY"), work)
	})

	if got != nil {
		t.Errorf("work succeeded, so the returned error should be nil; got %v", got)
	}
	if !strings.Contains(out, "could not open a new TTY") {
		t.Errorf("the display failure should still surface as a diagnostic, got %q", out)
	}
	if !strings.Contains(out, "Linking") {
		t.Errorf("stderr should carry the title, got %q", out)
	}
}

func TestRunWithSpinnerStatus_NonTTYRunsFn(t *testing.T) {
	SetQuiet(false)
	t.Cleanup(func() { SetQuiet(false) })

	ran := false
	var got error
	out := captureStderr(t, func() {
		got = RunWithSpinnerStatus("Fetching…", func(setStatus func(string)) error {
			setStatus("step 1")
			ran = true
			return nil
		})
	})

	if !ran {
		t.Fatal("fn should have been called")
	}
	if got != nil {
		t.Errorf("work succeeded, so the returned error should be nil; got %v", got)
	}
	if !strings.Contains(out, "Fetching…") {
		t.Errorf("stderr should carry the title, got %q", out)
	}
}
