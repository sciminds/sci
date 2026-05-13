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
	"github.com/sciminds/cli/internal/lab"
)

// sciMindsOrg is the Hugging Face organisation that gates sci cloud access.
const sciMindsOrg = "sciminds"

// hfWhoamiFn returns the authenticated HF user and their org memberships.
// Hits the Hugging Face API, so it can fail on slow/spotty networks even
// when the user is logged in — pair with [hfTokenPresentFn] for the
// "logged in?" question. Overridable in tests.
var hfWhoamiFn = func() (user string, orgs []string, err error) {
	if _, lookErr := exec.LookPath("hf"); lookErr != nil {
		return "", nil, lookErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, runErr := exec.CommandContext(ctx, "hf", "auth", "whoami").Output()
	if runErr != nil {
		return "", nil, runErr
	}
	return parseHFWhoami(string(out))
}

// hfTokenPresentFn reports whether a Hugging Face credential exists locally.
// This is the "are you logged in?" source of truth — `hf auth login` writes
// the token to disk, and the check itself doesn't hit the network. Pairs
// with [hfWhoamiFn] which is needed for org membership but flakes on
// slow connections. Overridable in tests.
var hfTokenPresentFn = func() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// HF_HOME overrides the default ~/.cache/huggingface location.
	hfHome := os.Getenv("HF_HOME")
	if hfHome == "" {
		hfHome = filepath.Join(home, ".cache", "huggingface")
	}
	info, statErr := os.Stat(filepath.Join(hfHome, "token"))
	return statErr == nil && info.Size() > 0
}

// gitXetRegisteredFn reports whether `git xet install` has wired the xet
// transfer agent into the global git config. Overridable in tests.
var gitXetRegisteredFn = func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "--global", "--get", "lfs.customtransfer.xet.path").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// gitXetInstallFn runs `git xet install` to register the xet transfer agent
// in the global git config. No auth required — it just writes git config —
// so doctor self-heals by running it whenever the binary is present but the
// agent isn't wired up. Overridable in tests.
var gitXetInstallFn = func() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "git", "xet", "install").Run()
}

// parseHFWhoami parses the single-line output of `hf auth whoami`:
//
//	user=ejolly orgs=py-feat,nltools,sciminds
//
// orgs is empty when the user has no org memberships.
func parseHFWhoami(s string) (string, []string, error) {
	user, orgs := "", []string(nil)
	for _, tok := range strings.Fields(strings.TrimSpace(s)) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch k {
		case "user":
			user = v
		case "orgs":
			if v != "" {
				orgs = strings.Split(v, ",")
			}
		}
	}
	if user == "" {
		return "", nil, fmt.Errorf("unexpected hf whoami output: %q", s)
	}
	return user, orgs, nil
}

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

	// Hugging Face auth — gates sci cloud. Always shown so first-time users
	// see the nudge. The local token presence is the source of truth for
	// "logged in" (no network); `hf whoami` is opportunistic for org
	// membership, with a last-good cache so transient network blips don't
	// flip the check between Pass and Warn.
	// HF auth is a warn (not fail) when missing: sci cloud needs it, but the
	// rest of sci works fine without it, and we don't want first-run / CI
	// machines to trip AllPassed on a credential that's an opt-in for one
	// subcommand.
	hfCheck := CheckResult{Label: "Hugging Face auth"}
	switch {
	case !hfTokenPresentFn():
		hfCheck.Status = StatusWarn
		hfCheck.Message = "not authenticated — run: hf auth login"
	default:
		user, orgs, hfErr := hfWhoamiFn()
		if hfErr == nil {
			writeHFCache(user, orgs)
		} else if cachedUser, cachedOrgs, ok := readHFCache(); ok {
			user, orgs, hfErr = cachedUser, cachedOrgs, nil
		}
		switch {
		case hfErr != nil:
			// Token present, whoami failed, no cache. Trust the token —
			// it's hard evidence of a prior successful login. Org check
			// is best-effort; the cloud command does its own verification
			// at use time.
			hfCheck.Status = StatusPass
			hfCheck.Message = "logged in"
		case !lo.Contains(orgs, sciMindsOrg):
			hfCheck.Status = StatusWarn
			hfCheck.Message = "@" + user + " — not in " + sciMindsOrg + " org"
		default:
			hfCheck.Status = StatusPass
			hfCheck.Message = "@" + user + " (" + sciMindsOrg + ")"
		}
	}
	checks = append(checks, hfCheck)

	// git-xet — required for HF bucket transfers. `git xet install` is just
	// a git-config write (no auth, no network), so if the binary is present
	// but the agent isn't wired up, self-heal by running it instead of
	// nagging the user.
	xetCheck := CheckResult{Label: "git-xet"}
	_, xetLookErr := exec.LookPath("git-xet")
	switch {
	case xetLookErr != nil:
		xetCheck.Status = StatusFail
		xetCheck.Message = "git-xet not found — run: brew install git-xet"
	case gitXetRegisteredFn():
		xetCheck.Status = StatusPass
		xetCheck.Message = "registered"
	default:
		if err := gitXetInstallFn(); err == nil && gitXetRegisteredFn() {
			xetCheck.Status = StatusPass
			xetCheck.Message = "registered (auto-installed)"
		} else {
			xetCheck.Status = StatusFail
			xetCheck.Message = "could not register — run: git xet install"
		}
	}
	checks = append(checks, xetCheck)

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
