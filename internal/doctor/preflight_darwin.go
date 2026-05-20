//go:build darwin

package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
)

// checkPreflight verifies Homebrew, Xcode CLT, and the user's default shell.
// macOS-flavored: brew is the package manager, Xcode CLT supplies the
// compiler toolchain pixi/uv lean on, and zsh is the assumed login shell.
func checkPreflight() CheckSection {
	var checks []CheckResult

	// Homebrew
	_, brewErr := exec.LookPath("brew")
	brewMsg := "installed"
	if brewErr != nil {
		brewMsg = "not installed — visit https://brew.sh"
	}
	checks = append(checks, CheckResult{
		Label: "Homebrew", Status: boolStatus(brewErr == nil), Message: brewMsg,
	})

	// Xcode CLT
	xcodePassed := exec.Command("xcode-select", "-p").Run() == nil
	xcodeMsg := "installed"
	if !xcodePassed {
		xcodeMsg = "not installed — run: xcode-select --install"
	}
	checks = append(checks, CheckResult{
		Label: "Xcode CLT", Status: boolStatus(xcodePassed), Message: xcodeMsg,
	})

	// Shell
	shell := os.Getenv("SHELL")
	if filepath.Base(shell) == "zsh" {
		checks = append(checks, CheckResult{
			Label: "Shell", Status: StatusPass, Message: "zsh",
		})
	} else {
		shellName := "not set"
		if shell != "" {
			shellName = filepath.Base(shell)
		}
		checks = append(checks, CheckResult{
			Label: "Shell", Status: StatusWarn, Message: shellName + " — expected zsh",
		})
	}

	return CheckSection{Name: "Pre-flight", Checks: checks}
}
