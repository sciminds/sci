// Package doctor checks the user's system for required tools and configuration.
//
// It runs three categories of checks concurrently:
//
//   - Pre-flight: package manager + compiler/shell environment (platform-specific)
//   - Identity: git user.name/email, GitHub CLI auth, SciMinds auth
//   - Tools: all packages from the embedded Brewfile via system snapshot
//
// Platform layout: [checkPreflight] is split across `preflight_darwin.go`
// and `preflight_linux.go`; [checkIdentity] lives in `identity.go` and is
// cross-platform.
package doctor

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
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
// Uses [brew.CollectSnapshotForBrewfile] so casks installed manually (drag
// into /Applications, vendor .pkg installers) aren't flagged as missing.
func RunToolChecks(r brew.Runner) ([]ToolInfo, error) {
	snap, err := brew.CollectSnapshotForBrewfile(r, Brewfile)
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
// Helpers
// ---------------------------------------------------------------------------

// boolStatus converts a boolean into [StatusPass] or [StatusFail].
func boolStatus(ok bool) Status {
	if ok {
		return StatusPass
	}
	return StatusFail
}
