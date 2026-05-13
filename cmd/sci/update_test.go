package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/sciminds/cli/internal/selfupdate"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

// TestUpdate_ExecsIntoDoctorAfterSuccessfulUpdate verifies that runUpdate
// chains into the freshly installed binary's `sci doctor --skip-upgrade-check`
// when a binary update is applied. This is how `sci update` picks up newly
// required tools (e.g. git-xet, hf for the HF cloud backend).
//
// runUpdate is called directly with a bare cli.Command to bypass the root
// command's Before hook (which would reset uikit's quiet flag).
func TestUpdate_ExecsIntoDoctorAfterSuccessfulUpdate(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{
			Available:   true,
			CurrentSHA:  "aaaaaaa",
			LatestSHA:   "bbbbbbb",
			DownloadURL: "https://example.invalid/sci",
		}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "/tmp/sci-new", nil
	}

	var execedWith string
	execAfterUpdate = func(execPath string) error {
		execedWith = execPath
		return nil
	}

	if err := runUpdate(context.Background(), &cli.Command{}); err != nil {
		t.Fatalf("runUpdate returned error: %v", err)
	}

	if execedWith != "/tmp/sci-new" {
		t.Errorf("execAfterUpdate not called with replaced binary path; got %q", execedWith)
	}
}

// TestUpdate_NoExecWhenAlreadyUpToDate verifies that an "up to date" check
// does NOT trigger the doctor re-exec — there's no new binary to chain into.
func TestUpdate_NoExecWhenAlreadyUpToDate(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{
			Available:  false,
			CurrentSHA: "aaaaaaa",
			LatestSHA:  "aaaaaaa",
		}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "", fmt.Errorf("Update() must not be called when sci is up to date")
	}

	var execed bool
	execAfterUpdate = func(_ string) error {
		execed = true
		return nil
	}

	if err := runUpdate(context.Background(), &cli.Command{}); err != nil {
		t.Fatalf("runUpdate returned error: %v", err)
	}

	if execed {
		t.Error("execAfterUpdate must not run when sci is already up to date")
	}
}

// TestUpdate_ExecFailureIsNonFatal verifies that an execAfterUpdate failure
// AFTER a successful binary replace does NOT cause runUpdate to return an
// error — the update itself completed atomically, and scripts wrapping
// `sci update` must see exit code 0.
//
// This is the "impossible to bork an update" invariant: once the binary
// is swapped, the post-update chain is best-effort.
func TestUpdate_ExecFailureIsNonFatal(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{
			Available:   true,
			CurrentSHA:  "aaaaaaa",
			LatestSHA:   "bbbbbbb",
			DownloadURL: "https://example.invalid/sci",
		}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "/tmp/sci-new", nil
	}

	// Simulate every plausible exec failure: missing binary, permission
	// denied, exec format error. None of these should leak as an error
	// from runUpdate.
	execAfterUpdate = func(_ string) error {
		return fmt.Errorf("fork/exec /tmp/sci-new: no such file or directory")
	}

	if err := runUpdate(context.Background(), &cli.Command{}); err != nil {
		t.Fatalf("runUpdate must not propagate exec failures (would imply the update itself failed): %v", err)
	}
}

// TestUpdate_DownloadFailurePropagates verifies the symmetric case: a
// failure DURING the binary replace (not after) IS an update failure and
// must surface as an error so the user knows their old binary is intact
// and the update did not happen.
func TestUpdate_DownloadFailurePropagates(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{
			Available:   true,
			CurrentSHA:  "aaaaaaa",
			LatestSHA:   "bbbbbbb",
			DownloadURL: "https://example.invalid/sci",
		}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "", fmt.Errorf("download: connection refused")
	}

	var execed bool
	execAfterUpdate = func(_ string) error {
		execed = true
		return nil
	}

	err := runUpdate(context.Background(), &cli.Command{})
	if err == nil {
		t.Fatal("expected runUpdate to return an error when the binary replace fails")
	}
	if execed {
		t.Error("execAfterUpdate must not run when the binary replace failed")
	}
}

// TestUpdate_CheckFailurePropagates verifies that an upstream Check error
// (e.g. GitHub API down) surfaces — no binary was replaced, so the user
// must know the update didn't happen.
func TestUpdate_CheckFailurePropagates(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{Error: "GitHub API returned 502"}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "", fmt.Errorf("Update() must not be called when Check failed")
	}
	execAfterUpdate = func(_ string) error {
		return fmt.Errorf("execAfterUpdate must not be called when Check failed")
	}

	err := runUpdate(context.Background(), &cli.Command{})
	if err == nil {
		t.Fatal("expected runUpdate to return an error when Check fails")
	}
}

// TestUpdate_JSONSkipsExec verifies that --json mode short-circuits before
// the doctor chain — scripts consuming `sci update --json` must not be
// hijacked by an interactive doctor flow.
func TestUpdate_JSONSkipsExec(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	origCheck, origUpdate, origExec := selfupdateCheck, selfupdateUpdate, execAfterUpdate
	t.Cleanup(func() {
		selfupdateCheck = origCheck
		selfupdateUpdate = origUpdate
		execAfterUpdate = origExec
	})

	selfupdateCheck = func() selfupdate.CheckResult {
		return selfupdate.CheckResult{
			Available:   true,
			CurrentSHA:  "aaaaaaa",
			LatestSHA:   "bbbbbbb",
			DownloadURL: "https://example.invalid/sci",
		}
	}
	selfupdateUpdate = func(_ string) (string, error) {
		return "", fmt.Errorf("Update() must not be called in --json mode")
	}

	var execed bool
	execAfterUpdate = func(_ string) error {
		execed = true
		return nil
	}

	// --json is plumbed at the root, so we exercise the JSON path through
	// the real command tree. Before hook sets quiet=true for --json, so
	// the spinner is suppressed.
	root := buildRoot()
	if err := root.Run(context.Background(), []string{"sci", "--json", "update"}); err != nil {
		t.Fatalf("runUpdate --json returned error: %v", err)
	}

	if execed {
		t.Error("execAfterUpdate must not run under --json")
	}
}
