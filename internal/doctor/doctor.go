// Package doctor checks the user's system for required tools and configuration.
//
// It runs three categories of checks concurrently:
//
//   - Pre-flight: Homebrew, Xcode CLT, shell environment
//   - Identity: git user.name/email, GitHub CLI auth, SciMinds auth
//   - Tools: all packages from the embedded Brewfile via system snapshot
package doctor

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/lab"
)

//go:embed Brewfile
var Brewfile string //nolint:revive // go:embed requires exported var

//go:embed BrewfileOptional
var BrewfileOptional string //nolint:revive // go:embed requires exported var

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Status represents the outcome of a single check.
type Status string

// Check outcome statuses.
const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
)

// CheckResult is a single pass/fail/warn check.
type CheckResult struct {
	Label   string
	Status  Status
	Message string
}

// CheckSection groups related checks under a heading.
type CheckSection struct {
	Name   string
	Checks []CheckResult
}

// ToolInfo reports whether a Brewfile package is installed.
type ToolInfo struct {
	Name      string
	Installed bool
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

// checkFuncs is the ordered list of check modules executed by [RunPreflightIdentity].
var checkFuncs = []func() CheckSection{
	checkPreflight,
	checkIdentity,
}

// RunPreflightIdentity runs all check modules concurrently and returns sections in order.
func RunPreflightIdentity() []CheckSection {
	sections := make([]CheckSection, len(checkFuncs))
	var wg sync.WaitGroup
	wg.Add(len(checkFuncs))

	for i, fn := range checkFuncs {
		go func(idx int, f func() CheckSection) {
			defer wg.Done()
			sections[idx] = f()
		}(i, fn)
	}

	wg.Wait()
	return sections
}

// RunToolChecks checks the system for required (embedded) packages by
// collecting a system snapshot and comparing against the embedded Brewfile.
func RunToolChecks(r brew.Runner) ([]ToolInfo, error) {
	snap, err := brew.CollectSnapshot(r)
	if err != nil {
		return nil, fmt.Errorf("check tools: %w", err)
	}

	entries := brew.ParseBrewfileEntries(Brewfile)
	infos := lo.Map(entries, func(e brew.BrewfileEntry, _ int) ToolInfo {
		return ToolInfo{Name: e.Name, Installed: snap.IsInstalled(e.Type, e.Name)}
	})
	return infos, nil
}

// ---------------------------------------------------------------------------
// Pre-flight checks
// ---------------------------------------------------------------------------

// checkPreflight verifies Homebrew, Xcode CLT, and the user's default shell.
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

// ---------------------------------------------------------------------------
// Identity checks
// ---------------------------------------------------------------------------

// checkIdentity verifies git user.name, user.email, gh auth, and SciMinds auth.
func checkIdentity() CheckSection {
	name := gitConfigValue("user.name")
	nameCheck := CheckResult{Label: "Git user.name"}
	if name != "" {
		nameCheck.Status = StatusPass
		nameCheck.Message = name
	} else {
		nameCheck.Status = StatusFail
		nameCheck.Message = "not set — run: git config --global user.name \"Your Name\""
	}

	email := gitConfigValue("user.email")
	emailCheck := CheckResult{Label: "Git user.email"}
	if email != "" {
		emailCheck.Status = StatusPass
		emailCheck.Message = email
	} else {
		emailCheck.Status = StatusFail
		emailCheck.Message = "not set — run: git config --global user.email you@example.com"
	}

	// gh auth token reads the local keyring/config — no network required.
	// This avoids failing the check when the user is offline.
	ghCheck := CheckResult{Label: "GitHub CLI auth"}
	ghToken, ghErr := exec.Command("gh", "auth", "token").Output()
	if ghErr == nil && strings.TrimSpace(string(ghToken)) != "" {
		ghCheck.Status = StatusPass
		ghCheck.Message = "logged into github"
	} else if _, lookErr := exec.LookPath("gh"); lookErr != nil {
		ghCheck.Status = StatusFail
		ghCheck.Message = "gh not found — install via: brew install gh"
	} else {
		ghCheck.Status = StatusFail
		ghCheck.Message = "not authenticated — run: gh auth login"
	}

	checks := []CheckResult{nameCheck, emailCheck, ghCheck}

	// SciMinds R2 credentials — always shown so first-time users see the nudge.
	sciCheck := CheckResult{Label: "SciMinds Public Cloud"}
	cfg, cfgErr := cloud.LoadConfig()
	switch {
	case cfgErr != nil:
		sciCheck.Status = StatusFail
		sciCheck.Message = "credentials unreadable — run: sci cloud setup"
	case cfg != nil && cfg.Public != nil && cfg.Public.AccessKey != "":
		sciCheck.Status = StatusPass
		sciCheck.Message = "configured as @" + cfg.Username
	default:
		sciCheck.Status = StatusWarn
		sciCheck.Message = "not authenticated — run: sci cloud setup"
	}
	checks = append(checks, sciCheck)

	// Lab SSH — only shown if configured. Checks local config + key, not connectivity.
	if labCfg, _ := lab.LoadConfig(); labCfg != nil && labCfg.User != "" {
		labCheck := CheckResult{Label: "Lab SSH"}
		home, _ := os.UserHomeDir()
		hasKey := sshKeyExists(home)
		hasAlias := sshAliasExists(home, labCfg.SSHAlias())
		switch {
		case hasKey && hasAlias:
			labCheck.Status = StatusPass
			labCheck.Message = "configured as " + labCfg.User + "@" + lab.Host
		case !hasKey:
			labCheck.Status = StatusFail
			labCheck.Message = "SSH key not found — run: sci lab setup"
		default:
			labCheck.Status = StatusFail
			labCheck.Message = "SSH config alias missing — run: sci lab setup"
		}
		checks = append(checks, labCheck)
	}

	return CheckSection{Name: "Identity", Checks: checks}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sshKeyExists returns true if any common SSH private key exists in ~/.ssh/.
func sshKeyExists(home string) bool {
	sshDir := filepath.Join(home, ".ssh")
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		if _, err := os.Stat(filepath.Join(sshDir, name)); err == nil {
			return true
		}
	}
	return false
}

// sshAliasExists returns true if the given Host alias is present in ~/.ssh/config.
func sshAliasExists(home, alias string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`(?m)^Host\s+` + regexp.QuoteMeta(alias) + `\s*$`)
	return re.Match(data)
}

// boolStatus converts a boolean into [StatusPass] or [StatusFail].
func boolStatus(ok bool) Status {
	if ok {
		return StatusPass
	}
	return StatusFail
}

// gitConfigValue reads a single git config --global value.
func gitConfigValue(key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
