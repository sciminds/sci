package cliui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestHint_QuietWritesToStderr(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			Hint("would install packages")
		})
		if !strings.Contains(stderr, "would install packages") {
			t.Errorf("Hint in quiet mode should write to stderr, got stderr=%q", stderr)
		}
	})

	if strings.Contains(stdout, "would install packages") {
		t.Error("Hint in quiet mode should NOT write to stdout")
	}
}

func TestOK_QuietWritesToStderr(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			OK("done")
		})
		if !strings.Contains(stderr, "done") {
			t.Errorf("OK in quiet mode should write to stderr, got stderr=%q", stderr)
		}
	})

	if strings.Contains(stdout, "done") {
		t.Error("OK in quiet mode should NOT write to stdout")
	}
}

func TestHint_NormalWritesToStdout(t *testing.T) {
	SetQuiet(false)

	stdout := captureStdout(t, func() {
		Hint("would install packages")
	})

	if !strings.Contains(stdout, "would install packages") {
		t.Errorf("Hint in normal mode should write to stdout, got %q", stdout)
	}
}
