package doctor

import (
	"os"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

// TestRunSetup_CreatedBrewfile tests that RunSetup dumps system state into a
// newly created Brewfile, checks required packages, and installs missing tools.
func TestRunSetup_CreatedBrewfile(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

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
}

// TestRunSetup_ExistingBrewfile tests the sync path for a pre-existing Brewfile.
func TestRunSetup_ExistingBrewfile(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

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
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

	tmpFile := writeTmpBrewfile(t, `brew "git"`)

	mock := &mockBrewRunner{
		missing:    []string{"uv"},
		installErr: os.ErrPermission,
	}

	result := RunSetup(mock, tmpFile, true)

	if result.InstallError == "" {
		t.Error("expected InstallError to be set when install fails")
	}
}

// TestRunSetup_NoMissingTools skips install when everything is present.
func TestRunSetup_NoMissingTools(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

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
