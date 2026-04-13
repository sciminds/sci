package cliui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr to a pipe, runs f, then returns what was written.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w

	f()

	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRunWithSpinner_QuietRunsFn(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	ran := false
	err := RunWithSpinner("loading…", func() error {
		ran = true
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("fn should have been called")
	}
}

func TestRunWithSpinner_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	want := errors.New("boom")
	got := RunWithSpinner("loading…", func() error {
		return want
	})

	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunWithSpinner_QuietPrintsTitleToStderr(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	out := captureStderr(t, func() {
		_ = RunWithSpinner("Checking updates…", func() error {
			return nil
		})
	})

	if !strings.Contains(out, "Checking updates…") {
		t.Errorf("stderr should contain title, got %q", out)
	}
}

func TestRunWithSpinnerStatus_QuietRunsFn(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	err := RunWithSpinnerStatus("installing…", func(setStatus func(string)) error {
		setStatus("step 1")
		setStatus("step 2")
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
