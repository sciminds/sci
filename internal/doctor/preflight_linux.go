//go:build linux

package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
)

// uvLookFn / gitLookFn are PATH probes for the Linux preflight. Exposed as
// package vars so tests can stub PATH state without manipulating $PATH.
var (
	uvLookFn  = func() error { _, err := exec.LookPath("uv"); return err }
	gitLookFn = func() error { _, err := exec.LookPath("git"); return err }
)

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
