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
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/brew"
	"github.com/sciminds/sci/internal/cloud"
)

// sciMindsOrg is the Hugging Face organisation that gates sci cloud access.
const sciMindsOrg = "sciminds"

// hfWhoamiFn returns the authenticated HF user and their org memberships.
// Delegates to [cloud.Whoami], which reads the local `hf` credential and asks
// the Hub directly rather than shelling out to `hf auth whoami`. It still hits
// the network, so it can fail on a slow link even when the user is logged in
// — pair it with [hfTokenPresentFn] for the "logged in?" question.
// Overridable in tests.
var hfWhoamiFn = cloud.Whoami

// hfTokenPresentFn reports whether a Hugging Face credential exists locally.
// This is the "are you logged in?" source of truth — `hf auth login` writes the
// token where [cloud.TokenPresent] reads it, and the check never touches the
// network. Pairs with [hfWhoamiFn], which is needed for org membership but
// depends on the Hub being reachable. Overridable in tests.
var hfTokenPresentFn = cloud.TokenPresent

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
