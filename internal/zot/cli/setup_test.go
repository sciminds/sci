package cli

// `sci zot setup` records one thing — where zotero.sqlite lives — and holds
// no credential of any kind. The flag that forgets it was called --logout,
// four lines above help text explaining that there is no account to log
// into. It is --clear, and the rename is deliberate: no alias, no shim, so
// a script passing the old name fails loudly instead of half-working.

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

// flagNames collects every name and alias a command declares.
func flagNames(cmd *cli.Command) []string {
	var out []string
	for _, f := range cmd.Flags {
		out = append(out, f.Names()...)
	}
	return out
}

func TestSetupCommand_ClearReplacesLogout(t *testing.T) {
	t.Parallel()
	cmd := setupCommand()
	names := flagNames(cmd)

	if !slices.Contains(names, "clear") {
		t.Errorf("setup declares no --clear flag; got %v", names)
	}
	if slices.Contains(names, "logout") {
		t.Errorf("--logout still parses; the rename is breaking on purpose, so no alias survives: %v", names)
	}
	if strings.Contains(cmd.Description, "--logout") {
		t.Errorf("setup --help still advertises --logout:\n%s", cmd.Description)
	}
	if !strings.Contains(cmd.Description, "--clear") {
		t.Errorf("setup --help does not show --clear:\n%s", cmd.Description)
	}
	for _, f := range cmd.Flags {
		if slices.Contains(f.Names(), "clear") && strings.Contains(strings.ToLower(f.String()), "log out") {
			t.Errorf("--clear's usage string still talks about logging out: %s", f.String())
		}
	}
}

// runSetupCmd runs `zot setup` with args against a root that mirrors the real
// mount, and returns the Run error.
func runSetupCmd(t *testing.T, args ...string) error {
	t.Helper()
	var jsonOut bool
	root := &cli.Command{
		Name:  "zot",
		Flags: []cli.Flag{cmdutil.JSONFlag(&jsonOut)},
		// Mirrors cmd/sci/root.go's Before: --json is what turns quiet mode
		// on, and quiet mode is what makes a confirmation refuse instead of
		// reaching for a TTY. Setting the global directly would test the
		// guard without testing the wiring that arms it.
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			return ctx, nil
		},
		Commands: []*cli.Command{setupCommand()},
	}
	t.Cleanup(func() { uikit.SetQuiet(false) })
	return root.Run(context.Background(), slices.Concat([]string{"zot", "setup"}, args))
}

// TestSetup_JSONOverwriteNeedsConsent pins the last instance of the
// "non-interactive is not consent" bug. The overwrite guard was
// `!jsonMode && !force`, so --json skipped the question entirely and
// rewrote a saved config — the comment above it said the caller "is
// expected to know what they're doing". Every other confirmation in the
// CLI stopped assuming that; this one is reached by a different route
// (an if-condition rather than cmdutil.Confirm's quiet check), so the
// contract has to be pinned here too.
func TestSetup_JSONOverwriteNeedsConsent(t *testing.T) {
	original := withTestConfig(t, "42", "6506098")
	replacement := t.TempDir()
	seedMinimalDB(t, replacement, "6506098")

	before, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if before == nil || before.DataDir != original {
		t.Fatalf("precondition: config should point at %q, got %+v", original, before)
	}

	runErr := runSetupCmd(t, "--json", "--data-dir", replacement)
	if runErr == nil {
		t.Fatal("setup --json overwrote an existing config without consent")
	}
	if !errors.Is(runErr, cmdutil.ErrNeedsConsent) {
		t.Fatalf("want ErrNeedsConsent, got %T: %v", runErr, runErr)
	}

	after, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after refusal: %v", err)
	}
	if after.DataDir != original {
		t.Errorf("config was rewritten despite the refusal: %q → %q", original, after.DataDir)
	}
}

// TestSetup_JSONOverwriteWithForce is the other half: --force is the consent
// flag, so the same command must go through. Without this the fix above
// would just make `setup --json` impossible to use on a configured machine.
func TestSetup_JSONOverwriteWithForce(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	replacement := t.TempDir()
	seedMinimalDB(t, replacement, "6506098")

	// stdout carries the result payload; keep it out of the test log.
	old := os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdout = devnull
	runErr := runSetupCmd(t, "--json", "--force", "--data-dir", replacement)
	os.Stdout = old
	_ = devnull.Close()

	if runErr != nil {
		t.Fatalf("setup --json --force: %v", runErr)
	}
	after, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if after.DataDir != replacement {
		t.Errorf("--force did not apply the new data dir: got %q, want %q", after.DataDir, replacement)
	}
}
