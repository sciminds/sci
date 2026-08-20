package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/uikit"
)

// TestDoctor_SkipUpgradeCheckFlagDefined asserts that the doctor command
// exposes --skip-upgrade-check for direct CLI use.
func TestDoctor_SkipUpgradeCheckFlagDefined(t *testing.T) {
	cmd := doctorCommand()
	for _, f := range cmd.Flags {
		if slices.Contains(f.Names(), "skip-upgrade-check") {
			return
		}
	}
	t.Error("doctor command missing --skip-upgrade-check flag")
}

// TestSkipUpgradeCheck_EnvVar asserts that SCI_SKIP_UPGRADE_CHECK=1 triggers
// the same suppression as the --skip-upgrade-check flag. This is what
// `sci update`'s re-exec sets so that a just-downloaded binary that
// predates the flag still gracefully skips the upgrade prompt.
func TestSkipUpgradeCheck_EnvVar(t *testing.T) {
	t.Setenv(postUpdateEnvVar, "1")
	if !skipUpgradeCheck(false) {
		t.Error("SCI_SKIP_UPGRADE_CHECK=1 must trigger skipUpgradeCheck()")
	}

	t.Setenv(postUpdateEnvVar, "")
	if skipUpgradeCheck(false) {
		t.Error("unset SCI_SKIP_UPGRADE_CHECK must not trigger skipUpgradeCheck()")
	}

	t.Setenv(postUpdateEnvVar, "0")
	if skipUpgradeCheck(false) {
		t.Error("SCI_SKIP_UPGRADE_CHECK=0 must not trigger skipUpgradeCheck()")
	}

	if !skipUpgradeCheck(true) {
		t.Error("the --skip-upgrade-check flag must trigger skipUpgradeCheck() even with the env var unset")
	}
}

// TestUpgradeCheckSkip covers the decision table behind the outdated check:
// the human flow still runs it, --json does not unless --yes consents to the
// package upgrade it authorizes, and every skip carries a reason for the
// envelope.
func TestUpgradeCheckSkip(t *testing.T) {
	t.Setenv(postUpdateEnvVar, "")

	cases := []struct {
		name             string
		flagSet          bool
		isJSON           bool
		yes              bool
		wantSkip         bool
		wantReasonSubstr string
	}{
		{name: "human default runs the check"},
		{name: "human with --skip-upgrade-check", flagSet: true, wantSkip: true, wantReasonSubstr: "--skip-upgrade-check"},
		{name: "json skips by default", isJSON: true, wantSkip: true, wantReasonSubstr: "--yes"},
		{name: "json with --yes runs the check", isJSON: true, yes: true},
		{name: "--skip-upgrade-check wins over --yes", flagSet: true, isJSON: true, yes: true, wantSkip: true, wantReasonSubstr: "--skip-upgrade-check"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			skip, reason := upgradeCheckSkip(tc.flagSet, tc.isJSON, tc.yes)
			if skip != tc.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tc.wantSkip)
			}
			if !tc.wantSkip {
				if reason != "" {
					t.Errorf("expected no reason when the check runs, got %q", reason)
				}
				return
			}
			if !strings.Contains(reason, tc.wantReasonSubstr) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.wantReasonSubstr)
			}
		})
	}
}

// TestUpgradeCheckSkip_EnvVar keeps the `sci update` re-exec hook wired to the
// same decision: SCI_SKIP_UPGRADE_CHECK=1 suppresses the check with a reason.
func TestUpgradeCheckSkip_EnvVar(t *testing.T) {
	t.Setenv(postUpdateEnvVar, "1")
	skip, reason := upgradeCheckSkip(false, false, false)
	if !skip {
		t.Fatal("SCI_SKIP_UPGRADE_CHECK=1 must suppress the upgrade check")
	}
	if reason == "" {
		t.Error("expected a reason for the skip")
	}
}

func TestToolsReccs_JSONSkipsForm(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	root := buildRoot()

	// --json mode should not launch the interactive multi-select.
	// It will fail trying to access brew but should NOT fail on huh form.
	err := root.Run(context.Background(), []string{"sci", "--json", "tools", "reccs"})

	// We expect either success or a brew-related error, NOT a TTY/huh error.
	if err != nil && strings.Contains(err.Error(), "TTY") {
		t.Errorf("--json mode should not open a TTY, got: %v", err)
	}
}

// TestToolsReccs_BulkMutex asserts that combining --install, --all,
// --include, or --exclude returns a CLI-level error before any backend work.
func TestToolsReccs_BulkMutex(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sci tools is darwin-only")
	}
	cases := [][]string{
		{"sci", "--json", "tools", "reccs", "--all", "--include", "bat"},
		{"sci", "--json", "tools", "reccs", "--all", "--exclude", "quarto"},
		{"sci", "--json", "tools", "reccs", "--include", "bat", "--exclude", "quarto"},
		{"sci", "--json", "tools", "reccs", "--install", "bat", "--all"},
	}
	for _, args := range cases {
		err := buildRoot().Run(context.Background(), args)
		if err == nil {
			t.Errorf("%v: expected mutex error, got nil", args)
			continue
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("%v: expected mutex error, got: %v", args, err)
		}
	}
}

func TestDoctor_JSONIncludesBrewfileFields(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("doctor brewfile fields are macOS-only")
	}
	if os.Getenv("SLOW") == "" {
		t.Skip("skipping integration test (set SLOW=1 to run)")
	}
	// Capture stdout to parse the JSON output.
	out := captureStdout(t, func() {
		root := buildRoot()
		// doctor --json may exit(1) if checks fail, but it should not panic or
		// skip the Brewfile/tools sections.
		_ = root.Run(context.Background(), []string{"sci", "--json", "doctor"})
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected valid JSON from doctor --json, got: %s", out)
	}

	// The key assertion: --json must include Brewfile/tools fields, not
	// bail early after pre-flight checks.
	if _, ok := result["brewfile_path"]; !ok {
		// brewfile_path is omitempty so it may be absent if Homebrew isn't
		// installed, but tools should always be present.
		if _, hasTools := result["tools"]; !hasTools {
			t.Error("doctor --json output missing both brewfile_path and tools — " +
				"likely still exiting early after pre-flight checks")
		}
	}
}
