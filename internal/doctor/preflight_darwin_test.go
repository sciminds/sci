//go:build darwin

package doctor

import (
	"strings"
	"testing"
)

// TestGitXetInstallHint_Darwin ensures the macOS install hint stays a brew
// formula. Mirrors TestGitXetInstallHint_Linux in preflight_linux_test.go.
func TestGitXetInstallHint_Darwin(t *testing.T) {
	hint := gitXetInstallHint()
	if !strings.Contains(hint, "brew install git-xet") {
		t.Errorf("darwin git-xet hint = %q, want brew install one-liner", hint)
	}
	if strings.Contains(hint, "curl") {
		t.Errorf("darwin git-xet hint should not mention curl: %q", hint)
	}
}

func TestCheckPreflight_Structure(t *testing.T) {
	sec := checkPreflight()

	if sec.Name != "Pre-flight" {
		t.Errorf("section name = %q, want %q", sec.Name, "Pre-flight")
	}
	if len(sec.Checks) != 3 {
		t.Fatalf("expected 3 checks (Homebrew, Xcode CLT, Shell), got %d", len(sec.Checks))
	}

	wantLabels := []string{"Homebrew", "Xcode CLT", "Shell"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
	}
}

func TestCheckPreflight_ShellUnset(t *testing.T) {
	t.Setenv("SHELL", "")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found in pre-flight section")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q when SHELL is empty", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "not set — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "not set — expected zsh")
	}
}

func TestCheckPreflight_NonZshShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q for bash", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "bash — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "bash — expected zsh")
	}
}

func TestCheckPreflight_BrewMissing(t *testing.T) {
	// Hide brew from PATH by setting PATH to an empty directory.
	t.Setenv("PATH", t.TempDir())

	sec := checkPreflight()

	var brewCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Homebrew" {
			brewCheck = &sec.Checks[i]
			break
		}
	}
	if brewCheck == nil {
		t.Fatal("Homebrew check not found")
	}
	if brewCheck.Status != StatusFail {
		t.Errorf("Homebrew status = %q, want %q when brew not in PATH", brewCheck.Status, StatusFail)
	}
	if brewCheck.Message != "not installed — visit https://brew.sh" {
		t.Errorf("Homebrew message = %q", brewCheck.Message)
	}
}
