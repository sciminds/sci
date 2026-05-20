package doctor

// identity.go — git/gh/HF/git-xet/lab identity checks. Cross-platform; the
// only platform-specific bit is the install hint for git-xet, switched on
// runtime.GOOS rather than via build tags (single-line difference).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/lab"
)

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
		xetCheck.Message = "git-xet not found — " + gitXetInstallHint()
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

// gitXetInstallHint returns the platform-appropriate install command for
// git-xet. Surfaced inline in the failing-check message.
func gitXetInstallHint() string {
	if runtime.GOOS == "linux" {
		return "run: curl --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/huggingface/xet-core/refs/heads/main/git_xet/install.sh | sh"
	}
	return "run: brew install git-xet"
}

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
