//go:build linux

package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
)

// uvLookFn / gitLookFn are PATH probes for the Linux preflight. Exposed as
// package vars so tests can stub PATH state without manipulating $PATH.
//
// uv's official installer drops the binary at ~/.local/bin/uv and expects
// the user's shell rc to add that directory to PATH. Non-interactive shells
// (ssh foo 'sci doctor', systemd units, CI runners) often skip the rc, so
// LookPath alone false-fails on machines where uv is in fact installed.
// We probe known per-user install dirs as a fallback before giving up.
var (
	uvLookFn  = func() error { return lookOrProbe("uv", userBinCandidates("uv")) }
	gitLookFn = func() error { _, err := exec.LookPath("git"); return err }
)

// lookOrProbe returns nil if `name` is on PATH OR if any of the candidate
// paths exists. Used to detect per-user installs (~/.local/bin, ~/.cargo/bin)
// that fall outside the non-interactive shell's PATH.
func lookOrProbe(name string, candidates []string) error {
	if _, err := exec.LookPath(name); err == nil {
		return nil
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return nil
		}
	}
	return &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// userBinCandidates returns the known per-user install paths to check for
// a binary when it's missing from PATH.
func userBinCandidates(name string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "bin", name), // uv installer default
		filepath.Join(home, ".cargo", "bin", name), // cargo-installed binaries
	}
}

// checkPreflight verifies uv, git, and the user's default shell. Linux-flavored:
// no brew assumption (apt/dnf/pacman are out of scope), no Xcode equivalent
// (we don't probe for build-essentials), and bash *or* zsh both pass — most
// Linux distros ship bash by default and there's no reason to nag.
func checkPreflight() CheckSection {
	var checks []CheckResult

	// uv — provides Python and the rest of the sci-installed tooling.
	uvErr := uvLookFn()
	uvMsg := "installed"
	if uvErr != nil {
		uvMsg = "not installed — run: curl -LsSf https://astral.sh/uv/install.sh | sh"
	}
	checks = append(checks, CheckResult{
		Label: "uv", Status: boolStatus(uvErr == nil), Message: uvMsg,
	})

	// git — every sci command that touches a repo needs it.
	gitErr := gitLookFn()
	gitMsg := "installed"
	if gitErr != nil {
		gitMsg = "not installed — use your distro package manager (apt/dnf/pacman)"
	}
	checks = append(checks, CheckResult{
		Label: "git", Status: boolStatus(gitErr == nil), Message: gitMsg,
	})

	// Shell — bash and zsh both pass on Linux. We only warn when SHELL is
	// unset or pointing at something unusual.
	shell := os.Getenv("SHELL")
	switch filepath.Base(shell) {
	case "bash", "zsh":
		checks = append(checks, CheckResult{
			Label: "Shell", Status: StatusPass, Message: filepath.Base(shell),
		})
	default:
		shellName := "not set"
		if shell != "" {
			shellName = filepath.Base(shell)
		}
		checks = append(checks, CheckResult{
			Label: "Shell", Status: StatusWarn, Message: shellName + " — expected bash or zsh",
		})
	}

	return CheckSection{Name: "Pre-flight", Checks: checks}
}
