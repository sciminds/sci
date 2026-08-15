package main

// consent_json_test.go — guards the `--json` consent contract at the command
// layer. cmdutil owns the refusal itself (see internal/cmdutil/confirm.go);
// these tests prove that (a) a destructive command actually aborts under
// --json rather than proceeding, and (b) every such command still offers a way
// to consent non-interactively, so the fix doesn't just trade a silent
// overwrite for an unusable command.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

// quietJSON puts the process in the state `sci … --json` runs in, with
// SCI_ASSUME cleared so an inherited value can't answer the prompt for us.
func quietJSON(t *testing.T) {
	t.Helper()
	t.Setenv("SCI_ASSUME", "")
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })
}

// TestVidConfirmOverwrite_JSONRefusesInsteadOfClobbering is the concrete
// hazard: `sci vid convert --json out.mp4` used to answer its own "already
// exists. Overwrite?" prompt and hand ffmpeg a doomed file. The gate must now
// refuse, and the existing file must survive untouched.
func TestVidConfirmOverwrite_JSONRefusesInsteadOfClobbering(t *testing.T) {
	quietJSON(t)

	out := filepath.Join(t.TempDir(), "lecture.mp4")
	const original = "the original video bytes"
	if err := os.WriteFile(out, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err := vidConfirmOverwrite(out, false)
	if err == nil {
		t.Fatal("vidConfirmOverwrite returned nil under --json — that is a silent overwrite")
	}
	if !errors.Is(err, cmdutil.ErrNeedsConsent) {
		t.Fatalf("want ErrNeedsConsent, got: %v", err)
	}
	// Not ErrCancelled: every vid runner maps that to `return nil`, which
	// would exit 0 having done nothing.
	if errors.Is(err, cmdutil.ErrCancelled) {
		t.Error("must not be ErrCancelled — the vid runners swallow that and exit 0")
	}

	got, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("output file was modified: got %q, want %q", got, original)
	}
}

// TestVidConfirmOverwrite_YesConsents is the other half: --yes is a real
// escape hatch, so `sci vid convert --json --yes` still works.
func TestVidConfirmOverwrite_YesConsents(t *testing.T) {
	quietJSON(t)

	out := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(out, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vidConfirmOverwrite(out, true); err != nil {
		t.Errorf("--yes should consent, got: %v", err)
	}
}

// TestVidConfirmOverwrite_AssumeYesConsents proves SCI_ASSUME is honoured even
// under quiet mode — the ordering bug this change fixes ran the quiet
// short-circuit first, so SCI_ASSUME=no was silently overridden with "yes".
func TestVidConfirmOverwrite_AssumeYesConsents(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	out := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(out, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SCI_ASSUME", "yes")
	if err := vidConfirmOverwrite(out, false); err != nil {
		t.Errorf("SCI_ASSUME=yes should consent under --json, got: %v", err)
	}

	t.Setenv("SCI_ASSUME", "no")
	err := vidConfirmOverwrite(out, false)
	if !errors.Is(err, cmdutil.ErrCancelled) {
		t.Errorf("SCI_ASSUME=no should decline (ErrCancelled), got: %v", err)
	}
	if errors.Is(err, cmdutil.ErrNeedsConsent) {
		t.Error("an explicit SCI_ASSUME=no is an answer, not a missing one")
	}
}

// TestConsentFlags_EveryGatedCommandCanConsent walks the commands that can
// reach a confirmation prompt and requires each to declare a flag granting
// consent. Without one, refusing to auto-confirm under --json would turn the
// command into a dead end — a different bug, not a fix.
//
// The flag name is checked against every alias, so `--force` carrying a
// `yes` alias (cloud put) satisfies it: the remedy cmdutil prints says
// "re-run with --yes", and it has to be true wherever it's printed.
func TestConsentFlags_EveryGatedCommandCanConsent(t *testing.T) {
	root := buildRoot()

	// path → the flag that grants consent non-interactively.
	cases := []struct {
		path []string
		flag string
	}{
		{[]string{"vid"}, "yes"}, // shared parent flag, propagates to every verb
		{[]string{"db", "reset"}, "yes"},
		{[]string{"db", "delete"}, "yes"},
		{[]string{"db", "rename"}, "yes"},
		{[]string{"cass", "push"}, "yes"},
		{[]string{"cass", "revert"}, "yes"},
		{[]string{"cass", "canvas", "modules"}, "yes"},
		{[]string{"cass", "canvas", "assignments"}, "yes"},
		{[]string{"cass", "canvas", "announce"}, "yes"},
		{[]string{"cloud", "put"}, "yes"},
		{[]string{"cloud", "remove"}, "yes"},
		{[]string{"doctor"}, "yes"},
	}

	for _, tc := range cases {
		t.Run(strings.Join(tc.path, " "), func(t *testing.T) {
			cmd := lookupCmd(root.Commands, tc.path)
			if cmd == nil {
				t.Fatalf("command %q not found", strings.Join(tc.path, " "))
			}
			if !hasFlag(cmd, tc.flag) {
				t.Errorf("sci %s reaches a confirmation prompt but declares no --%s: "+
					"under --json it would refuse with no way to consent",
					strings.Join(tc.path, " "), tc.flag)
			}
		})
	}
}

// lookupCmd walks a command path (e.g. ["cass","canvas","modules"]).
func lookupCmd(cmds []*cli.Command, path []string) *cli.Command {
	var found *cli.Command
	for _, name := range path {
		found = findCmd(cmds, name)
		if found == nil {
			return nil
		}
		cmds = found.Commands
	}
	return found
}
