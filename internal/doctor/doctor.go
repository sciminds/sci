// Package doctor checks the user's system for required tools and configuration.
//
// It runs three categories of checks concurrently:
//
//   - Pre-flight: Homebrew, Xcode CLT, shell environment
//   - Identity: git user.name/email, GitHub CLI auth, SciMinds auth
//   - Tools: all packages from the embedded Brewfile via `brew bundle check`
package doctor

import (
	"cmp"
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cloud"
)

//go:embed Brewfile
var Brewfile string

//go:embed BrewfileOptional
var BrewfileOptional string

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Status represents the outcome of a single check.
type Status string

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

// checkFuncs is the ordered list of check modules executed by [RunAll].
var checkFuncs = []func() CheckSection{
	checkPreflight,
	checkIdentity,
}

// RunAll runs all check modules concurrently and returns sections in order.
func RunAll() []CheckSection {
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

// RunToolChecks writes the embedded Brewfile to a temp file and runs
// `brew bundle check` to identify missing packages.
func RunToolChecks(r brew.Runner) []ToolInfo {
	tmpFile, err := writeTempBrewfile()
	if err != nil {
		return nil
	}
	defer func() { _ = os.Remove(tmpFile) }()

	missing, err := r.BundleCheck(tmpFile)
	if err != nil {
		return nil
	}
	missingSet := make(map[string]bool, len(missing))
	for _, name := range missing {
		missingSet[name] = true
	}

	all := parseBrewfileNames(Brewfile)
	infos := make([]ToolInfo, len(all))
	for i, name := range all {
		infos[i] = ToolInfo{Name: name, Installed: !missingSet[name]}
	}
	return infos
}

// InstallAll runs `brew bundle install` with the embedded Brewfile.
func InstallAll(r brew.Runner) (string, error) {
	tmpFile, err := writeTempBrewfile()
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile) }()
	return r.BundleInstall(tmpFile)
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
		shellName := cmp.Or(filepath.Base(shell), "unknown")
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

	ghCheck := CheckResult{Label: "GitHub CLI auth"}
	ghOut, ghErr := exec.Command("gh", "auth", "status").CombinedOutput()
	if ghErr == nil {
		re := regexp.MustCompile(`account\s+(\S+)`)
		if m := re.FindSubmatch(ghOut); len(m) >= 2 {
			ghCheck.Message = "authenticated as " + string(m[1])
		} else {
			ghCheck.Message = "authenticated"
		}
		ghCheck.Status = StatusPass
	} else {
		ghCheck.Status = StatusFail
		ghCheck.Message = "not authenticated — run: gh auth login"
	}

	checks := []CheckResult{nameCheck, emailCheck, ghCheck}

	// SciMinds R2 credentials — only shown if configured.
	if cfg, _ := cloud.LoadConfig(); cfg != nil && cfg.Public != nil && cfg.Public.AccessKey != "" {
		authCheck := CheckResult{Label: "SciMinds R2"}
		authCheck.Status = StatusPass
		authCheck.Message = "configured as @" + cfg.Username
		checks = append(checks, authCheck)
	}

	return CheckSection{Name: "Identity", Checks: checks}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// parseBrewfileNames extracts package names from a Brewfile in declaration order.
func parseBrewfileNames(content string) []string {
	var names []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Lines like: brew "git" or cask "visual-studio-code"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := strings.Trim(parts[1], `"`)
			names = append(names, name)
		}
	}
	return names
}

// BrewfileEntry is a parsed line from a Brewfile.
type BrewfileEntry struct {
	Type string // "brew", "cask", "uv"
	Name string // package name without quotes/extras (e.g. "git", "symbex")
	Line string // original Brewfile line for writing back
}

// Label returns a display label like "git (brew)" or "symbex (uv)".
func (e BrewfileEntry) Label() string {
	return e.Name + " (" + e.Type + ")"
}

// parseBrewfileEntries parses a Brewfile into structured entries.
func parseBrewfileEntries(content string) []BrewfileEntry {
	var entries []BrewfileEntry
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		typ := parts[0]
		// Strip quotes and any trailing extras like [recommended] or , with: [...]
		name := strings.Trim(parts[1], `",`)
		// Strip bracket suffixes like "marimo[recommended]"
		if idx := strings.Index(name, "["); idx != -1 {
			name = name[:idx]
		}
		entries = append(entries, BrewfileEntry{Type: typ, Name: name, Line: trimmed})
	}
	return entries
}

// writeTempBrewfile writes the embedded Brewfile to a temp file and returns its path.
func writeTempBrewfile() (string, error) {
	return writeTempBrewfileContent(Brewfile)
}

func writeTempBrewfileContent(content string) (string, error) {
	f, err := os.CreateTemp("", "sci-doctor-Brewfile-*")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	_ = f.Close()
	return f.Name(), nil
}
