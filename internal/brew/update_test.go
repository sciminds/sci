package brew

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUpdate_UpgradesOutdated(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "htop", InstalledVersion: "3.3.0", CurrentVersion: "3.4.0"},
		},
		upgradeOut: "==> Upgrading htop\n",
	}

	result, err := Update(m, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.updateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", m.updateCalls)
	}
	if m.upgradeCalls != 1 {
		t.Errorf("expected 1 upgrade call, got %d", m.upgradeCalls)
	}
	if result.CheckOnly {
		t.Error("expected CheckOnly=false")
	}
	if len(result.Outdated) != 1 || result.Outdated[0].Name != "htop" {
		t.Errorf("unexpected outdated: %+v", result.Outdated)
	}
}

func TestUpdate_CheckOnly(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "curl", InstalledVersion: "8.8.0", CurrentVersion: "8.9.0"},
		},
	}

	result, err := Update(m, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.updateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", m.updateCalls)
	}
	if m.upgradeCalls != 0 {
		t.Errorf("expected 0 upgrade calls, got %d", m.upgradeCalls)
	}
	if !result.CheckOnly {
		t.Error("expected CheckOnly=true")
	}
	if len(result.Outdated) != 1 {
		t.Errorf("expected 1 outdated, got %d", len(result.Outdated))
	}
}

func TestUpdate_NothingOutdated(t *testing.T) {
	t.Parallel()
	m := &mockRunner{}

	result, err := Update(m, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.upgradeCalls != 0 {
		t.Errorf("expected 0 upgrade calls when nothing outdated, got %d", m.upgradeCalls)
	}
	if len(result.Outdated) != 0 {
		t.Errorf("expected no outdated, got %d", len(result.Outdated))
	}
}

func TestUpdate_IncludesUVOutdated(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "htop", InstalledVersion: "3.3.0", CurrentVersion: "3.4.0"},
		},
		uvOutdatedResult: []OutdatedPackage{
			{Name: "ruff", InstalledVersion: "0.14.0", CurrentVersion: "0.15.9"},
		},
		upgradeOut:   "==> Upgrading htop\n",
		uvUpgradeOut: "Updated ruff\n",
	}

	result, err := Update(m, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Outdated) != 2 {
		t.Fatalf("expected 2 outdated (1 brew + 1 uv), got %d", len(result.Outdated))
	}
	if m.upgradeCalls != 1 {
		t.Errorf("expected 1 brew upgrade call, got %d", m.upgradeCalls)
	}
	if m.uvUpgradeCalls != 1 {
		t.Errorf("expected 1 uv upgrade call, got %d", m.uvUpgradeCalls)
	}
}

func TestUpdate_CheckOnly_IncludesUV(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "curl", InstalledVersion: "8.8.0", CurrentVersion: "8.9.0"},
		},
		uvOutdatedResult: []OutdatedPackage{
			{Name: "marimo", InstalledVersion: "0.22.4", CurrentVersion: "0.23.0"},
		},
	}

	result, err := Update(m, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Outdated) != 2 {
		t.Fatalf("expected 2 outdated, got %d", len(result.Outdated))
	}
	if m.upgradeCalls != 0 {
		t.Errorf("expected 0 brew upgrade calls, got %d", m.upgradeCalls)
	}
	if m.uvUpgradeCalls != 0 {
		t.Errorf("expected 0 uv upgrade calls, got %d", m.uvUpgradeCalls)
	}
}

func TestUpdate_OnlyUVOutdated(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		uvOutdatedResult: []OutdatedPackage{
			{Name: "ruff", InstalledVersion: "0.14.0", CurrentVersion: "0.15.9"},
		},
		uvUpgradeOut: "Updated ruff\n",
	}

	result, err := Update(m, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Outdated) != 1 {
		t.Fatalf("expected 1 outdated, got %d", len(result.Outdated))
	}
	if m.upgradeCalls != 0 {
		t.Errorf("expected 0 brew upgrade calls when only uv outdated, got %d", m.upgradeCalls)
	}
	if m.uvUpgradeCalls != 1 {
		t.Errorf("expected 1 uv upgrade call, got %d", m.uvUpgradeCalls)
	}
}

func TestUpdate_BrewFailureDoesNotStrandUV(t *testing.T) {
	// Regression: a brew upgrade failure (e.g. a cask sudo prompt timing out)
	// used to early-return before uv upgrade ran, leaving uv tools as still
	// outdated. Now both phases run; the error covers brew, but uv still
	// gets its upgrade call.
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "htop", InstalledVersion: "3.3.0", CurrentVersion: "3.4.0"},
		},
		uvOutdatedResult: []OutdatedPackage{
			{Name: "ruff", InstalledVersion: "0.14.0", CurrentVersion: "0.15.9"},
		},
		upgradeErr: errors.New("brew upgrade: sudo timed out"),
	}

	_, err := Update(m, false)
	if err == nil {
		t.Fatal("expected brew upgrade error, got nil")
	}
	if !strings.Contains(err.Error(), "brew upgrade") {
		t.Errorf("error = %q, want it to mention brew upgrade", err.Error())
	}
	if m.upgradeCalls != 1 {
		t.Errorf("expected 1 brew upgrade call, got %d", m.upgradeCalls)
	}
	if m.uvUpgradeCalls != 1 {
		t.Errorf("expected 1 uv upgrade call (continued past brew failure), got %d", m.uvUpgradeCalls)
	}
}

func TestUpdate_UVUpgradePreservesBracketExtras(t *testing.T) {
	// Regression: when a uv tool is declared in the Brewfile with bracket
	// extras (e.g. `uv "marimo[recommended]"`), Update must pass the spec
	// — not the bare name — to UVUpgrade. Otherwise the underlying
	// `uv tool install <name> --upgrade` reinstall silently drops the extras.
	// t.Setenv prevents t.Parallel here.
	dir := t.TempDir()
	bf := filepath.Join(dir, "Brewfile")
	if err := os.WriteFile(bf, []byte(`uv "marimo[recommended]"`+"\n"+`uv "hf"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEBREW_BUNDLE_FILE_GLOBAL", bf)

	m := &mockRunner{
		uvOutdatedResult: []OutdatedPackage{
			{Name: "marimo", InstalledVersion: "0.23.0", CurrentVersion: "0.24.0"},
			{Name: "hf", InstalledVersion: "1.8.0", CurrentVersion: "1.15.0"},
		},
	}

	if _, err := Update(m, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.uvUpgradeCalls != 1 {
		t.Fatalf("expected 1 uv upgrade call, got %d", m.uvUpgradeCalls)
	}
	want := []string{"marimo[recommended]", "hf"}
	if !slices.Equal(m.uvUpgradeArgs[0], want) {
		t.Errorf("UVUpgrade specs = %v, want %v", m.uvUpgradeArgs[0], want)
	}
}

func TestResolveUVSpecs_NoBrewfile(t *testing.T) {
	// When no Brewfile is found, names pass through unchanged so the upgrade
	// path still works on a system without one. t.Setenv prevents t.Parallel.
	t.Setenv("HOMEBREW_BUNDLE_FILE_GLOBAL", "/nonexistent/Brewfile")
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent")
	t.Setenv("HOME", t.TempDir())

	got := ResolveUVSpecs([]string{"marimo", "hf"})
	want := []string{"marimo", "hf"}
	if !slices.Equal(got, want) {
		t.Errorf("ResolveUVSpecs = %v, want %v", got, want)
	}
}

func TestUpdate_UpdateFails(t *testing.T) {
	t.Parallel()
	m := &mockRunner{updateErr: errors.New("network error")}

	_, err := Update(m, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if m.upgradeCalls != 0 {
		t.Errorf("should not upgrade when update fails")
	}
}

func TestUpgradeOnly_HappyPath(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "htop", InstalledVersion: "3.3.0", CurrentVersion: "3.4.0"},
		},
		uvOutdatedResult: []OutdatedPackage{
			{Name: "ruff", InstalledVersion: "0.14.0", CurrentVersion: "0.15.9"},
		},
	}

	result, err := UpgradeOnly(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT call Update (registry refresh).
	if m.updateCalls != 0 {
		t.Errorf("expected 0 update calls (no registry refresh), got %d", m.updateCalls)
	}
	if m.upgradeCalls != 1 {
		t.Errorf("expected 1 brew upgrade call, got %d", m.upgradeCalls)
	}
	if m.uvUpgradeCalls != 1 {
		t.Errorf("expected 1 uv upgrade call, got %d", m.uvUpgradeCalls)
	}
	if len(result.Outdated) != 2 {
		t.Errorf("expected 2 outdated, got %d", len(result.Outdated))
	}
}

func TestUpgradeOnly_NothingOutdated(t *testing.T) {
	t.Parallel()
	m := &mockRunner{}

	result, err := UpgradeOnly(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.upgradeCalls != 0 {
		t.Errorf("expected 0 upgrade calls, got %d", m.upgradeCalls)
	}
	if len(result.Outdated) != 0 {
		t.Errorf("expected 0 outdated, got %d", len(result.Outdated))
	}
}

// ---------------------------------------------------------------------------
// Sync deterministic order test
// ---------------------------------------------------------------------------
