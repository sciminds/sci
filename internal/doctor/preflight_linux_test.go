//go:build linux

package doctor

import (
	"fmt"
	"strings"
	"testing"
)

// setUvLook / setGitLook swap the Linux preflight's PATH probes for a test.
// Pattern mirrors setGitXetRegistered / setHFWhoami in doctor_test.go.
func setUvLook(t *testing.T, err error) {
	t.Helper()
	orig := uvLookFn
	uvLookFn = func() error { return err }
	t.Cleanup(func() { uvLookFn = orig })
}

func setGitLook(t *testing.T, err error) {
	t.Helper()
	orig := gitLookFn
	gitLookFn = func() error { return err }
	t.Cleanup(func() { gitLookFn = orig })
}

func TestCheckPreflight_Structure_Linux(t *testing.T) {
	setUvLook(t, nil)
	setGitLook(t, nil)
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()

	if sec.Name != "Pre-flight" {
		t.Errorf("section name = %q, want %q", sec.Name, "Pre-flight")
	}
	if len(sec.Checks) != 3 {
		t.Fatalf("expected 3 checks (uv, git, Shell), got %d", len(sec.Checks))
	}
	wantLabels := []string{"uv", "git", "Shell"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}
	for _, c := range sec.Checks {
		if c.Status != StatusPass {
			t.Errorf("check %q status = %q, want Pass", c.Label, c.Status)
		}
	}
}

func TestCheckPreflight_Linux_UvMissing(t *testing.T) {
	setUvLook(t, fmt.Errorf("not found"))
	setGitLook(t, nil)
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()
	c := findCheck(sec, "uv")
	if c == nil {
		t.Fatal("uv check missing")
	}
	if c.Status != StatusFail {
		t.Errorf("uv status = %q, want Fail when missing", c.Status)
	}
	if !strings.Contains(c.Message, "astral.sh/uv/install.sh") {
		t.Errorf("uv message = %q, want curl install hint", c.Message)
	}
}

func TestCheckPreflight_Linux_GitMissing(t *testing.T) {
	setUvLook(t, nil)
	setGitLook(t, fmt.Errorf("not found"))
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()
	c := findCheck(sec, "git")
	if c == nil {
		t.Fatal("git check missing")
	}
	if c.Status != StatusFail {
		t.Errorf("git status = %q, want Fail when missing", c.Status)
	}
	if !strings.Contains(c.Message, "apt") {
		t.Errorf("git message = %q, want distro-package hint", c.Message)
	}
}

func TestCheckPreflight_Linux_BashAndZshBothPass(t *testing.T) {
	setUvLook(t, nil)
	setGitLook(t, nil)
	tests := []struct{ shell, want string }{
		{"/bin/bash", "bash"},
		{"/usr/bin/zsh", "zsh"},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			sec := checkPreflight()
			c := findCheck(sec, "Shell")
			if c == nil {
				t.Fatal("Shell check missing")
			}
			if c.Status != StatusPass {
				t.Errorf("Shell status = %q, want Pass for %s", c.Status, tt.shell)
			}
			if c.Message != tt.want {
				t.Errorf("Shell message = %q, want %q", c.Message, tt.want)
			}
		})
	}
}

func TestCheckPreflight_Linux_ShellUnset(t *testing.T) {
	setUvLook(t, nil)
	setGitLook(t, nil)
	t.Setenv("SHELL", "")

	sec := checkPreflight()
	c := findCheck(sec, "Shell")
	if c == nil {
		t.Fatal("Shell check missing")
	}
	if c.Status != StatusWarn {
		t.Errorf("Shell status = %q, want Warn when SHELL unset", c.Status)
	}
	if !strings.Contains(c.Message, "not set") {
		t.Errorf("Shell message = %q, want 'not set' phrasing", c.Message)
	}
}

// TestGitXetInstallHint_Linux ensures the git-xet install hint surfaces the
// curl one-liner on Linux (not the brew formula). Lives next to the preflight
// tests because the runtime.GOOS branch in identity.go has no platform-
// independent way to exercise it.
func TestGitXetInstallHint_Linux(t *testing.T) {
	hint := gitXetInstallHint()
	if !strings.Contains(hint, "curl") {
		t.Errorf("Linux git-xet hint = %q, want curl install one-liner", hint)
	}
	if strings.Contains(hint, "brew install") {
		t.Errorf("Linux git-xet hint should not mention brew: %q", hint)
	}
}

func findCheck(sec CheckSection, label string) *CheckResult {
	for i := range sec.Checks {
		if sec.Checks[i].Label == label {
			return &sec.Checks[i]
		}
	}
	return nil
}
