package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/brew"
	"github.com/sciminds/sci/internal/sciconfig"
	"github.com/sciminds/sci/internal/uikit"
)

// writeIgnoreConfig makes content the tools.json this test reads.
func writeIgnoreConfig(t *testing.T, content string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	path := sciconfig.Path("tools.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func outdatedGitMock() *mockBrewRunner {
	return &mockBrewRunner{
		listFormulaeResult: allRequiredFormulae(),
		listCasksResult:    allRequiredCasks(),
		uvToolListResult:   allRequiredUV(),
		outdated:           []brew.OutdatedPackage{{Type: brew.TypeFormula, Name: "git", InstalledVersion: "2.44", CurrentVersion: "2.45"}},
	}
}

func TestRunSetup_SkipsIgnored(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)
	writeIgnoreConfig(t, `{"ignored":[{"type":"formula","name":"git"}]}`)

	mock := outdatedGitMock()
	result := RunSetup(t.Context(), mock, writeTmpBrewfile(t, Brewfile), false, SetupOpts{})

	if mock.upgradeCalls != 0 {
		t.Errorf("brew upgrade calls = %d, want 0", mock.upgradeCalls)
	}
	if len(result.Ignored) != 1 || result.Ignored[0].Name != "git" {
		t.Errorf("Ignored = %v, want git", result.Ignored)
	}
	if len(result.Outdated) != 0 || len(result.Upgraded) != 0 {
		t.Errorf("Outdated = %v, Upgraded = %v, want both empty", result.Outdated, result.Upgraded)
	}
}

func TestRunSetup_MalformedIgnoreList(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)
	writeIgnoreConfig(t, `{"ignored":`)

	mock := outdatedGitMock()
	result := RunSetup(t.Context(), mock, writeTmpBrewfile(t, Brewfile), false, SetupOpts{})

	if result.UpgradeCheck.Status != UpgradeCheckFailed {
		t.Errorf("UpgradeCheck.Status = %q, want %q", result.UpgradeCheck.Status, UpgradeCheckFailed)
	}
	if !strings.Contains(result.UpdateError, "tools.json") {
		t.Errorf("UpdateError = %q, want it to name tools.json", result.UpdateError)
	}
	if mock.upgradeCalls != 0 || mock.uvUpgCalls != 0 {
		t.Errorf("upgrade calls = %d brew, %d uv, want none", mock.upgradeCalls, mock.uvUpgCalls)
	}
}
