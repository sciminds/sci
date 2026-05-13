package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
)

// TestDoctor_SkipUpgradeCheckFlagDefined asserts that the doctor command
// exposes --skip-upgrade-check for direct CLI use.
func TestDoctor_SkipUpgradeCheckFlagDefined(t *testing.T) {
	cmd := doctorCommand()
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == "skip-upgrade-check" {
				return
			}
		}
	}
	t.Error("doctor command missing --skip-upgrade-check flag")
}

// TestSkipUpgradeCheck_EnvVar asserts that SCI_SKIP_UPGRADE_CHECK=1 triggers
// the same suppression as the --skip-upgrade-check flag. This is what
// `sci update`'s re-exec sets so that a just-downloaded binary that
// predates the flag still gracefully skips the upgrade prompt.
func TestSkipUpgradeCheck_EnvVar(t *testing.T) {
	prev := doctorSkipUpgradeCheck
	t.Cleanup(func() { doctorSkipUpgradeCheck = prev })
	doctorSkipUpgradeCheck = false

	t.Setenv(postUpdateEnvVar, "1")
	if !skipUpgradeCheck() {
		t.Error("SCI_SKIP_UPGRADE_CHECK=1 must trigger skipUpgradeCheck()")
	}

	t.Setenv(postUpdateEnvVar, "")
	if skipUpgradeCheck() {
		t.Error("unset SCI_SKIP_UPGRADE_CHECK must not trigger skipUpgradeCheck()")
	}

	t.Setenv(postUpdateEnvVar, "0")
	if skipUpgradeCheck() {
		t.Error("SCI_SKIP_UPGRADE_CHECK=0 must not trigger skipUpgradeCheck()")
	}

	doctorSkipUpgradeCheck = true
	if !skipUpgradeCheck() {
		t.Error("the --skip-upgrade-check flag must trigger skipUpgradeCheck() even with the env var unset")
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

func TestDoctor_JSONIncludesBrewfileFields(t *testing.T) {
	if os.Getenv("SLOW") == "" {
		t.Skip("skipping integration test (set SLOW=1 to run)")
	}
	// Capture stdout to parse the JSON output.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := buildRoot()
	// doctor --json may exit(1) if checks fail, but it should not panic or
	// skip the Brewfile/tools sections.
	_ = root.Run(context.Background(), []string{"sci", "--json", "doctor"})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON from doctor --json, got: %s", buf.String())
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
