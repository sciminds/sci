package doctor

import (
	"fmt"
	"testing"

	"github.com/sciminds/cli/internal/brew"
)

func TestBoolStatus(t *testing.T) {
	if got := boolStatus(true); got != StatusPass {
		t.Errorf("boolStatus(true) = %q, want %q", got, StatusPass)
	}
	if got := boolStatus(false); got != StatusFail {
		t.Errorf("boolStatus(false) = %q, want %q", got, StatusFail)
	}
}

func TestCheckPreflight_Structure(t *testing.T) {
	sec := checkPreflight()

	if sec.Name != "Pre-flight" {
		t.Errorf("section name = %q, want %q", sec.Name, "Pre-flight")
	}
	if len(sec.Checks) != 3 {
		t.Fatalf("expected 3 checks (Homebrew, Xcode CLT, Shell), got %d", len(sec.Checks))
	}

	wantLabels := []string{"Homebrew", "Xcode CLT", "Shell"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
	}
}

func TestCheckIdentity_Structure(t *testing.T) {
	sec := checkIdentity()

	if sec.Name != "Identity" {
		t.Errorf("section name = %q, want %q", sec.Name, "Identity")
	}

	if len(sec.Checks) < 3 {
		t.Fatalf("expected at least 3 checks, got %d", len(sec.Checks))
	}

	wantLabels := []string{"Git user.name", "Git user.email", "GitHub CLI auth"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
		if c.Message == "" {
			t.Errorf("check %q has empty message", c.Label)
		}
	}
}

func TestRunPreflightIdentity_ReturnsSections(t *testing.T) {
	sections := RunPreflightIdentity()

	if len(sections) != len(checkFuncs) {
		t.Fatalf("RunPreflightIdentity returned %d sections, want %d", len(sections), len(checkFuncs))
	}

	for i, sec := range sections {
		if sec.Name == "" {
			t.Errorf("section[%d] has empty name", i)
		}
		if len(sec.Checks) == 0 {
			t.Errorf("section %q has no checks", sec.Name)
		}
	}

	if sections[0].Name != "Pre-flight" {
		t.Errorf("first section = %q, want %q", sections[0].Name, "Pre-flight")
	}
	if sections[1].Name != "Identity" {
		t.Errorf("second section = %q, want %q", sections[1].Name, "Identity")
	}
}

func TestParseBrewfileNames(t *testing.T) {
	content := `brew "git"
brew "uv"
# a comment
cask "visual-studio-code"
`
	names := brew.ParseBrewfileNames(content)
	want := []string{"git", "uv", "visual-studio-code"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestParseBrewfileNames_Empty(t *testing.T) {
	names := brew.ParseBrewfileNames("")
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestBrewfileEmbedded(t *testing.T) {
	if Brewfile == "" {
		t.Fatal("embedded Brewfile is empty")
	}
	names := brew.ParseBrewfileNames(Brewfile)
	if len(names) == 0 {
		t.Fatal("embedded Brewfile has no packages")
	}
}

func TestRunToolChecks(t *testing.T) {
	mock := &mockBrewRunner{
		// Simulate: git, node installed as formulae; uv NOT installed.
		listFormulaeResult: []string{"git", "node", "ffmpeg", "gh", "openssh", "oven-sh/bun/bun", "pixi", "sqlite", "rsync"},
		listCasksResult:    []string{"1password", "slack", "visual-studio-code", "vlc", "zed", "zoom", "quarto"},
		uvToolListResult:   []string{"marimo", "mystmd"},
	}
	infos, err := RunToolChecks(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(infos) == 0 {
		t.Fatal("expected tool infos from embedded Brewfile")
	}

	// uv should be marked as not installed (missing from formulae list).
	for _, ti := range infos {
		if ti.Name == "uv" && ti.Installed {
			t.Error("expected uv to be marked as not installed")
		}
		if ti.Name == "git" && !ti.Installed {
			t.Error("expected git to be marked as installed")
		}
		if ti.Name == "node" && !ti.Installed {
			t.Error("expected node to be marked as installed")
		}
		if ti.Name == "marimo" && !ti.Installed {
			t.Error("expected marimo to be marked as installed")
		}
	}
}

// mockBrewRunner implements brew.Runner for testing.
// All error fields default to nil (no error), so existing tests are unaffected.
type mockBrewRunner struct {
	// Snapshot fields — used by CollectSnapshot via RunToolChecks.
	leavesResult       []string
	leavesErr          error
	listFormulaeResult []string
	listFormulaeErr    error
	listCasksResult    []string
	listCasksErr       error
	tapsResult         []string
	tapsErr            error
	uvToolListResult   []string
	uvToolListErr      error

	// Install tracking.
	installFormulaeCalls [][]string
	installFormulaeErr   error
	installCasksCalls    [][]string
	installCasksErr      error
	installUVToolsCalls  [][]string
	installUVToolsErr    error

	// Update/upgrade.
	outdated      []brew.OutdatedPackage
	outdatedErr   error
	uvOutdated    []brew.OutdatedPackage
	uvOutdatedErr error
	updateErr     error
	upgradeCalls  int
	upgradeErr    error
	uvUpgCalls    int
	uvUpgradeErr  error
}

func (m *mockBrewRunner) Info(_ []string, _ bool) ([]brew.PackageInfo, error) {
	return nil, nil
}

func (m *mockBrewRunner) Leaves() ([]string, error) { return m.leavesResult, m.leavesErr }
func (m *mockBrewRunner) ListFormulae() ([]string, error) {
	return m.listFormulaeResult, m.listFormulaeErr
}
func (m *mockBrewRunner) ListCasks() ([]string, error)      { return m.listCasksResult, m.listCasksErr }
func (m *mockBrewRunner) Taps() ([]string, error)           { return m.tapsResult, m.tapsErr }
func (m *mockBrewRunner) DirectInstall(_, _ string) error   { return nil }
func (m *mockBrewRunner) DirectUninstall(_, _ string) error { return nil }
func (m *mockBrewRunner) InstallFormulae(names []string) error {
	m.installFormulaeCalls = append(m.installFormulaeCalls, names)
	return m.installFormulaeErr
}
func (m *mockBrewRunner) InstallCasks(names []string) error {
	m.installCasksCalls = append(m.installCasksCalls, names)
	return m.installCasksErr
}
func (m *mockBrewRunner) InstallUVTools(names []string) error {
	m.installUVToolsCalls = append(m.installUVToolsCalls, names)
	return m.installUVToolsErr
}
func (m *mockBrewRunner) UVToolList() ([]string, error) { return m.uvToolListResult, m.uvToolListErr }
func (m *mockBrewRunner) Update() error                 { return m.updateErr }
func (m *mockBrewRunner) Outdated() ([]brew.OutdatedPackage, error) {
	return m.outdated, m.outdatedErr
}
func (m *mockBrewRunner) Upgrade() (string, error) {
	m.upgradeCalls++
	return "", m.upgradeErr
}
func (m *mockBrewRunner) UVOutdated() ([]brew.OutdatedPackage, error) {
	return m.uvOutdated, m.uvOutdatedErr
}
func (m *mockBrewRunner) UVUpgrade(_ []string) (string, error) {
	m.uvUpgCalls++
	return "", m.uvUpgradeErr
}
func TestRunToolChecks_SnapshotError(t *testing.T) {
	mock := &mockBrewRunner{listFormulaeErr: fmt.Errorf("brew not installed")}
	infos, err := RunToolChecks(mock)
	if err == nil {
		t.Fatal("expected error from RunToolChecks when snapshot fails")
	}
	if infos != nil {
		t.Errorf("expected nil infos on error, got %v", infos)
	}
}

func TestCheckPreflight_ShellUnset(t *testing.T) {
	t.Setenv("SHELL", "")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found in pre-flight section")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q when SHELL is empty", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "not set — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "not set — expected zsh")
	}
}

func TestCheckPreflight_NonZshShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q for bash", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "bash — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "bash — expected zsh")
	}
}

func TestCheckPreflight_BrewMissing(t *testing.T) {
	// Hide brew from PATH by setting PATH to an empty directory.
	t.Setenv("PATH", t.TempDir())

	sec := checkPreflight()

	var brewCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Homebrew" {
			brewCheck = &sec.Checks[i]
			break
		}
	}
	if brewCheck == nil {
		t.Fatal("Homebrew check not found")
	}
	if brewCheck.Status != StatusFail {
		t.Errorf("Homebrew status = %q, want %q when brew not in PATH", brewCheck.Status, StatusFail)
	}
	if brewCheck.Message != "not installed — visit https://brew.sh" {
		t.Errorf("Homebrew message = %q", brewCheck.Message)
	}
}
