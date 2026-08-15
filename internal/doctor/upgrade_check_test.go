package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/brew"
	"github.com/sciminds/sci/internal/uikit"
)

// TestRunSetup_UpgradeCheckTimesOut is the regression test for `sci doctor
// --json` hanging: the brew/uv outdated check had no deadline, so a stalled
// `brew update` held the whole command — and with it the JSON envelope —
// indefinitely. RunSetup must abandon the check and return a complete,
// degraded result instead.
func TestRunSetup_UpgradeCheckTimesOut(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	mock := &mockBrewRunner{
		listFormulaeResult: allRequiredFormulae(),
		listCasksResult:    allRequiredCasks(),
		uvToolListResult:   allRequiredUV(),
		updateBlock:        block,
	}

	// Cancelled up front: proves the deadline is honored without depending
	// on wall-clock timing.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result := RunSetup(ctx, mock, tmpFile, false, SetupOpts{})

	if result.UpgradeCheck.Status != UpgradeCheckTimedOut {
		t.Errorf("UpgradeCheck.Status = %q, want %q", result.UpgradeCheck.Status, UpgradeCheckTimedOut)
	}
	if result.UpdateError == "" {
		t.Error("expected UpdateError to describe the timeout")
	}
	if len(result.Outdated) != 0 || len(result.Upgraded) != 0 {
		t.Errorf("expected no outdated/upgraded packages on timeout, got %v / %v",
			result.Outdated, result.Upgraded)
	}
	// The rest of the result must still be there — degraded, not absent.
	if len(result.Tools) == 0 {
		t.Error("expected Tools to be populated even when the upgrade check times out")
	}
	if result.BrewfilePath != tmpFile {
		t.Errorf("BrewfilePath = %q, want %q", result.BrewfilePath, tmpFile)
	}
}

// TestRunSetup_UpgradeCheckStatusOK records a completed check so machine
// consumers can tell "nothing outdated" from "we never looked".
func TestRunSetup_UpgradeCheckStatusOK(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)
	mock := &mockBrewRunner{
		listFormulaeResult: allRequiredFormulae(),
		listCasksResult:    allRequiredCasks(),
		uvToolListResult:   allRequiredUV(),
	}

	result := RunSetup(t.Context(), mock, tmpFile, false, SetupOpts{})

	if result.UpgradeCheck.Status != UpgradeCheckOK {
		t.Errorf("UpgradeCheck.Status = %q, want %q", result.UpgradeCheck.Status, UpgradeCheckOK)
	}
}

// TestRunSetup_UpgradeCheckStatusSkipped reports the skip explicitly rather
// than leaving the section silently empty.
func TestRunSetup_UpgradeCheckStatusSkipped(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)
	mock := &mockBrewRunner{
		listFormulaeResult: allRequiredFormulae(),
		listCasksResult:    allRequiredCasks(),
		uvToolListResult:   allRequiredUV(),
	}

	result := RunSetup(t.Context(), mock, tmpFile, false, SetupOpts{SkipUpgradeCheck: true})

	if result.UpgradeCheck.Status != UpgradeCheckSkipped {
		t.Errorf("UpgradeCheck.Status = %q, want %q", result.UpgradeCheck.Status, UpgradeCheckSkipped)
	}
	if result.UpgradeCheck.Hint == "" {
		t.Error("a skipped check must tell the caller how to run it")
	}
}

// TestRunSetup_UpgradeCheckFailed distinguishes a runner failure from a
// timeout — an agent branching on the envelope needs to tell them apart.
func TestRunSetup_UpgradeCheckFailed(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)
	mock := &mockBrewRunner{
		listFormulaeResult: allRequiredFormulae(),
		listCasksResult:    allRequiredCasks(),
		uvToolListResult:   allRequiredUV(),
		outdatedErr:        errBrewBroken,
	}

	result := RunSetup(t.Context(), mock, tmpFile, false, SetupOpts{})

	if result.UpgradeCheck.Status != UpgradeCheckFailed {
		t.Errorf("UpgradeCheck.Status = %q, want %q", result.UpgradeCheck.Status, UpgradeCheckFailed)
	}
	if result.UpdateError == "" {
		t.Error("expected UpdateError to be set when the check fails")
	}
}

// TestDocResult_JSONAlwaysCarriesUpgradeCheck asserts the envelope contract:
// upgrade_check is always present, so a consumer never has to infer from an
// absent field whether the check ran.
func TestDocResult_JSONAlwaysCarriesUpgradeCheck(t *testing.T) {
	for _, r := range []DocResult{
		{},
		{UpgradeCheck: UpgradeCheckReport{Status: UpgradeCheckTimedOut, Reason: "timed out"}},
	} {
		data, err := json.Marshal(r.JSON())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := decoded["upgrade_check"]; !ok {
			t.Fatalf("upgrade_check missing from envelope: %s", data)
		}
	}
}

// TestDocResult_HumanMentionsTimedOutCheck keeps the human path honest too:
// a check that was abandoned has to say so.
func TestDocResult_HumanMentionsTimedOutCheck(t *testing.T) {
	r := DocResult{
		Sections:     []CheckSection{{Name: "Pre-flight", Checks: []CheckResult{{Label: "Homebrew", Status: StatusPass}}}},
		UpgradeCheck: UpgradeCheckReport{Status: UpgradeCheckTimedOut, Reason: "timed out after 1m0s"},
	}
	if out := r.Human(); !strings.Contains(out, "timed out") {
		t.Errorf("Human() should mention the timed-out upgrade check, got:\n%s", out)
	}
}

// errBrewBroken stands in for a failing brew subprocess.
var errBrewBroken = errors.New("brew: command not found")

// compile-time guard: the mock must keep satisfying brew.Runner.
var _ brew.Runner = (*mockBrewRunner)(nil)
