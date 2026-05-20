package uikit

import (
	"errors"
	"strings"
	"testing"
)

// withClipboardStubs replaces both indirection points and restores them.
func withClipboardStubs(t *testing.T, candidates []clipboardCmd, run func(clipboardCmd, string) error) {
	t.Helper()
	origFn := clipboardCmdFn
	origRun := runClipboardCmd
	clipboardCmdFn = func() []clipboardCmd { return candidates }
	runClipboardCmd = run
	t.Cleanup(func() {
		clipboardCmdFn = origFn
		runClipboardCmd = origRun
	})
}

func TestCopy_NoCandidates(t *testing.T) {
	withClipboardStubs(t, nil, func(clipboardCmd, string) error { return nil })
	err := Copy("hi")
	if err == nil {
		t.Fatal("expected error when no candidates registered")
	}
	if !strings.Contains(err.Error(), "no clipboard tool available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCopy_TriesInOrderUntilSuccess(t *testing.T) {
	// Use real tool names so LookPath succeeds-or-fails based on the host;
	// the run hook decides whether the call "works". We pick names likely
	// to exist on the runner (`echo`, `cat`) so LookPath returns true.
	var calls []string
	withClipboardStubs(t,
		[]clipboardCmd{
			{Name: "echo"},
			{Name: "cat"},
		},
		func(c clipboardCmd, _ string) error {
			calls = append(calls, c.Name)
			if c.Name == "echo" {
				return errors.New("simulated failure")
			}
			return nil
		},
	)
	if err := Copy("payload"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(calls) != 2 || calls[0] != "echo" || calls[1] != "cat" {
		t.Errorf("expected echo→cat, got %v", calls)
	}
}

func TestCopy_SkipsMissingTools(t *testing.T) {
	// Mix a guaranteed-missing tool with a real one.
	var ran string
	withClipboardStubs(t,
		[]clipboardCmd{
			{Name: "sci-clipboard-definitely-not-installed-zzz"},
			{Name: "cat"},
		},
		func(c clipboardCmd, _ string) error {
			ran = c.Name
			return nil
		},
	)
	if err := Copy("x"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if ran != "cat" {
		t.Errorf("expected to run cat (after missing tool skipped), ran %q", ran)
	}
}

func TestCopy_AllFailReportsTried(t *testing.T) {
	withClipboardStubs(t,
		[]clipboardCmd{
			{Name: "sci-missing-clip-a-zzz"},
			{Name: "sci-missing-clip-b-zzz"},
		},
		func(clipboardCmd, string) error { return nil },
	)
	err := Copy("x")
	if err == nil {
		t.Fatal("expected error when no clipboard tool present")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sci-missing-clip-a-zzz") || !strings.Contains(msg, "sci-missing-clip-b-zzz") {
		t.Errorf("error should list every tried tool, got: %v", err)
	}
}

// TestCopy_PassesPayloadThrough verifies the input string reaches the run
// hook unchanged — guards against accidental trimming or buffering changes.
func TestCopy_PassesPayloadThrough(t *testing.T) {
	var got string
	withClipboardStubs(t,
		[]clipboardCmd{{Name: "cat"}},
		func(_ clipboardCmd, s string) error {
			got = s
			return nil
		},
	)
	want := "a\tb\nc\td\n"
	if err := Copy(want); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("payload differs: got %q, want %q", got, want)
	}
}
