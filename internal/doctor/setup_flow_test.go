package doctor

import (
	"fmt"
	"os"
	"testing"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
)

// TestRunSetup_CreatedBrewfile tests that RunSetup dumps system state into a
// newly created Brewfile, checks required packages, and installs missing tools.
func TestRunSetup_CreatedBrewfile(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	// Create a temp Brewfile with just git — simulates a fresh dump.
	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		missing: []string{"uv", "pixi"}, // BundleCheck will report these missing
	}

	result := RunSetup(mock, tmpFile, true)

	if result.BrewfilePath != tmpFile {
		t.Errorf("BrewfilePath = %q, want %q", result.BrewfilePath, tmpFile)
	}
	if !result.BrewfileCreated {
		t.Error("expected BrewfileCreated = true")
	}

	// Required packages not in the Brewfile should have been appended.
	if len(result.PackagesAdded) == 0 {
		t.Error("expected PackagesAdded to be non-empty")
	}

	// Tool checks should have run.
	if len(result.Tools) == 0 {
		t.Fatal("expected Tools to be populated")
	}

	// Missing tools should have been installed (auto-confirm in quiet mode).
	if len(result.ToolsInstalled) == 0 {
		t.Error("expected ToolsInstalled to be non-empty after auto-install")
	}
	if result.InstallError != "" {
		t.Errorf("unexpected InstallError: %s", result.InstallError)
	}

	// After a successful install, all Tools entries should be marked installed.
	for _, ti := range result.Tools {
		if !ti.Installed {
			t.Errorf("tool %q still marked as not installed after successful install", ti.Name)
		}
	}
}

// TestRunSetup_ExistingBrewfile tests the sync path for a pre-existing Brewfile.
func TestRunSetup_ExistingBrewfile(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	// Brewfile already has git — simulates an existing setup.
	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		missing: []string{}, // all tools installed
	}

	result := RunSetup(mock, tmpFile, false)

	if result.BrewfileCreated {
		t.Error("expected BrewfileCreated = false for existing Brewfile")
	}
	if result.InstallError != "" {
		t.Errorf("unexpected InstallError: %s", result.InstallError)
	}
}

// TestRunSetup_InstallError records install failures in InstallError.
func TestRunSetup_InstallError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		missing:    []string{"uv"},
		installErr: os.ErrPermission,
	}

	result := RunSetup(mock, tmpFile, true)

	if result.InstallError == "" {
		t.Error("expected InstallError to be set when install fails")
	}

	// Tools should remain not-installed when install fails.
	for _, ti := range result.Tools {
		if ti.Name == "uv" && ti.Installed {
			t.Error("tool uv should remain not-installed after failed install")
		}
	}
}

// TestRunSetup_NoMissingTools skips install when everything is present.
func TestRunSetup_NoMissingTools(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	// Write a Brewfile that already has all required packages.
	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing: []string{}, // nothing missing
	}

	result := RunSetup(mock, tmpFile, false)

	if len(result.ToolsInstalled) != 0 {
		t.Errorf("expected no tools installed, got %v", result.ToolsInstalled)
	}
	if result.InstallError != "" {
		t.Errorf("unexpected InstallError: %s", result.InstallError)
	}
}

// TestRunSetup_OutdatedUpgrade checks that outdated packages are detected and upgraded.
func TestRunSetup_OutdatedUpgrade(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing: []string{},
		outdated: []brew.OutdatedPackage{
			{Name: "git", InstalledVersion: "2.44.0", CurrentVersion: "2.45.0"},
		},
		uvOutdated: []brew.OutdatedPackage{
			{Name: "marimo", InstalledVersion: "0.22.4", CurrentVersion: "0.23.0"},
		},
	}

	result := RunSetup(mock, tmpFile, false)

	if len(result.Outdated) != 2 {
		t.Fatalf("expected 2 outdated packages, got %d", len(result.Outdated))
	}
	if len(result.Upgraded) != 2 {
		t.Fatalf("expected 2 upgraded packages, got %d", len(result.Upgraded))
	}
	if mock.upgradeCalls != 1 {
		t.Errorf("expected 1 brew upgrade call, got %d", mock.upgradeCalls)
	}
	if mock.uvUpgCalls != 1 {
		t.Errorf("expected 1 uv upgrade call, got %d", mock.uvUpgCalls)
	}
	if result.UpdateError != "" {
		t.Errorf("unexpected UpdateError: %s", result.UpdateError)
	}
}

// TestRunSetup_NothingOutdated verifies no upgrade when everything is current.
func TestRunSetup_NothingOutdated(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{missing: []string{}}

	result := RunSetup(mock, tmpFile, false)

	if len(result.Outdated) != 0 {
		t.Errorf("expected no outdated packages, got %d", len(result.Outdated))
	}
	if len(result.Upgraded) != 0 {
		t.Errorf("expected no upgraded packages, got %d", len(result.Upgraded))
	}
	if mock.upgradeCalls != 0 {
		t.Errorf("expected 0 brew upgrade calls, got %d", mock.upgradeCalls)
	}
}

// TestRunSetup_UpdateError records update failures in UpdateError.
func TestRunSetup_UpdateError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing:   []string{},
		updateErr: os.ErrPermission,
	}

	result := RunSetup(mock, tmpFile, false)

	if result.UpdateError == "" {
		t.Error("expected UpdateError to be set when brew update fails")
	}
}

// TestRunSetup_BrewOnlyUpgrade only calls brew upgrade when only brew packages are outdated.
func TestRunSetup_BrewOnlyUpgrade(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing: []string{},
		outdated: []brew.OutdatedPackage{
			{Name: "git", InstalledVersion: "2.44.0", CurrentVersion: "2.45.0"},
		},
	}

	result := RunSetup(mock, tmpFile, false)

	if len(result.Upgraded) != 1 {
		t.Fatalf("expected 1 upgraded package, got %d", len(result.Upgraded))
	}
	if mock.upgradeCalls != 1 {
		t.Errorf("expected 1 brew upgrade call, got %d", mock.upgradeCalls)
	}
	if mock.uvUpgCalls != 0 {
		t.Errorf("expected 0 uv upgrade calls, got %d", mock.uvUpgCalls)
	}
}

// ---------------------------------------------------------------------------
// Error-path tests: each exercises a failure mode in RunSetup.
// ---------------------------------------------------------------------------

// TestRunSetup_BundleCheckError verifies that when BundleCheck fails (e.g.
// brew is borked), ToolCheckError is populated and no install is attempted.
func TestRunSetup_BundleCheckError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		bundleCheckErr: fmt.Errorf("brew: command not found"),
	}

	result := RunSetup(mock, tmpFile, true)

	if result.ToolCheckError == "" {
		t.Fatal("expected ToolCheckError to be set when BundleCheck fails")
	}
	if result.Tools != nil {
		t.Errorf("expected nil Tools on BundleCheck error, got %v", result.Tools)
	}
	if len(result.ToolsInstalled) != 0 {
		t.Error("expected no tools installed when BundleCheck fails")
	}
	if len(mock.installCalls) != 0 {
		t.Error("BundleInstall should not be called when BundleCheck fails")
	}
}

// TestRunSetup_SyncError verifies that when Sync fails (e.g. brew leaves
// errors), the rest of the flow still runs.
func TestRunSetup_SyncError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		leavesErr: fmt.Errorf("brew leaves failed"),
		missing:   []string{},
	}

	result := RunSetup(mock, tmpFile, true)

	// Sync failed but flow should continue — tools should still be checked.
	if result.Tools == nil {
		t.Fatal("expected Tools to be populated even when sync fails")
	}
}

// TestRunSetup_SyncErrorExisting verifies that when Sync fails on an existing
// Brewfile, the rest of the flow still runs.
func TestRunSetup_SyncErrorExisting(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		leavesErr: fmt.Errorf("brew leaves failed"),
		missing:   []string{},
	}

	result := RunSetup(mock, tmpFile, false)

	// Sync failed but flow should continue.
	if result.Tools == nil {
		t.Fatal("expected Tools to be populated even when sync fails")
	}
}

// TestRunSetup_AppendError verifies that when required packages can't be
// added to the Brewfile, the error is recorded.
func TestRunSetup_AppendError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	// Create a Brewfile that's missing required packages, then make it
	// read-only so AppendEntries fails.
	tmpFile := writeTmpBrewfile(t, `brew "git"`)
	if err := os.Chmod(tmpFile, 0o444); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(tmpFile, 0o644) }()

	mock := &mockBrewRunner{missing: []string{}}

	result := RunSetup(mock, tmpFile, false)

	if result.AppendError == "" {
		t.Error("expected AppendError when Brewfile is read-only")
	}
}

// TestRunSetup_OutdatedBrewError verifies that a brew Outdated() error is
// recorded in UpdateError.
func TestRunSetup_OutdatedBrewError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing:     []string{},
		outdatedErr: fmt.Errorf("brew outdated: json parse error"),
	}

	result := RunSetup(mock, tmpFile, false)

	if result.UpdateError == "" {
		t.Fatal("expected UpdateError when Outdated() fails")
	}
}

// TestRunSetup_OutdatedUVError verifies that a uv UVOutdated() error is
// recorded in UpdateError even when brew Outdated() succeeds.
func TestRunSetup_OutdatedUVError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing:       []string{},
		uvOutdatedErr: fmt.Errorf("uv: command not found"),
	}

	result := RunSetup(mock, tmpFile, false)

	if result.UpdateError == "" {
		t.Fatal("expected UpdateError when UVOutdated() fails")
	}
}

// TestRunSetup_UpgradeError verifies that outdated packages are detected but
// when Upgrade() fails, no packages are marked as upgraded.
func TestRunSetup_UpgradeError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing: []string{},
		outdated: []brew.OutdatedPackage{
			{Name: "git", InstalledVersion: "2.44", CurrentVersion: "2.45"},
		},
		upgradeErr: fmt.Errorf("upgrade failed: permission denied"),
	}

	result := RunSetup(mock, tmpFile, false)

	if len(result.Outdated) == 0 {
		t.Error("expected Outdated to be populated")
	}
	if len(result.Upgraded) != 0 {
		t.Errorf("expected no Upgraded when upgrade fails, got %v", result.Upgraded)
	}
	if result.UpdateError == "" {
		t.Fatal("expected UpdateError when Upgrade() fails")
	}
}

// TestRunSetup_UVUpgradeError verifies that when brew upgrade succeeds but
// uv upgrade fails, the error is still recorded.
func TestRunSetup_UVUpgradeError(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, Brewfile)

	mock := &mockBrewRunner{
		missing: []string{},
		uvOutdated: []brew.OutdatedPackage{
			{Name: "marimo", InstalledVersion: "0.22", CurrentVersion: "0.23"},
		},
		uvUpgradeErr: fmt.Errorf("uv tool upgrade: network error"),
	}

	result := RunSetup(mock, tmpFile, false)

	if result.UpdateError == "" {
		t.Fatal("expected UpdateError when UVUpgrade() fails")
	}
}

func writeTmpBrewfile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "Brewfile-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return f.Name()
}
