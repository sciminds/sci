package brew

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Compile-time interface assertions.
var (
	_ Runner = BundleRunner{}
	_ Runner = (*mockRunner)(nil)
)

// mockRunner records calls and returns preset results.
// ListDetailed fans out BundleList across goroutines, so mutations to
// listCalls must be guarded.
type mockRunner struct {
	mu        sync.Mutex
	listCalls []mockCall

	installCalls []string
	installErr   error
	checkCalls   []string
	checkResult  []string
	checkErr     error
	listResult   []string
	listErr      error
	infoResult   []PackageInfo
	infoErr      error

	// Leaves-based sync fields.
	leavesResult       []string
	leavesErr          error
	listFormulaeResult []string
	listFormulaeErr    error
	listCasksResult    []string
	listCasksErr       error
	tapsResult         []string
	tapsErr            error

	// Direct install/uninstall tracking.
	directInstallCalls   []mockCall
	directInstallErr     error
	directUninstallCalls []mockCall
	directUninstallErr   error

	updateCalls      int
	updateErr        error
	outdatedResult   []OutdatedPackage
	outdatedErr      error
	upgradeCalls     int
	upgradeOut       string
	upgradeErr       error
	uvOutdatedResult []OutdatedPackage
	uvOutdatedErr    error
	uvUpgradeCalls   int
	uvUpgradeOut     string
	uvUpgradeErr     error
	uvToolListResult []string
	uvToolListErr    error
}

type mockCall struct {
	file, pkg, pkgType string
}

func (m *mockRunner) BundleInstall(file string) (string, error) {
	m.installCalls = append(m.installCalls, file)
	return "installed", m.installErr
}

func (m *mockRunner) BundleCheck(file string) ([]string, error) {
	m.checkCalls = append(m.checkCalls, file)
	return m.checkResult, m.checkErr
}

func (m *mockRunner) BundleList(file, pkgType string) ([]string, error) {
	m.mu.Lock()
	m.listCalls = append(m.listCalls, mockCall{file: file, pkgType: pkgType})
	m.mu.Unlock()
	return m.listResult, m.listErr
}

func (m *mockRunner) Info(_ []string, _ bool) ([]PackageInfo, error) {
	return m.infoResult, m.infoErr
}

func (m *mockRunner) Leaves() ([]string, error) {
	return m.leavesResult, m.leavesErr
}

func (m *mockRunner) ListFormulae() ([]string, error) {
	return m.listFormulaeResult, m.listFormulaeErr
}

func (m *mockRunner) ListCasks() ([]string, error) {
	return m.listCasksResult, m.listCasksErr
}

func (m *mockRunner) Taps() ([]string, error) {
	return m.tapsResult, m.tapsErr
}

func (m *mockRunner) DirectInstall(pkg, pkgType string) error {
	m.directInstallCalls = append(m.directInstallCalls, mockCall{pkg: pkg, pkgType: pkgType})
	return m.directInstallErr
}

func (m *mockRunner) DirectUninstall(pkg, pkgType string) error {
	m.directUninstallCalls = append(m.directUninstallCalls, mockCall{pkg: pkg, pkgType: pkgType})
	return m.directUninstallErr
}

func (m *mockRunner) Update() error {
	m.updateCalls++
	return m.updateErr
}

func (m *mockRunner) Outdated() ([]OutdatedPackage, error) {
	return m.outdatedResult, m.outdatedErr
}

func (m *mockRunner) Upgrade() (string, error) {
	m.upgradeCalls++
	return m.upgradeOut, m.upgradeErr
}

func (m *mockRunner) UVOutdated() ([]OutdatedPackage, error) {
	return m.uvOutdatedResult, m.uvOutdatedErr
}

func (m *mockRunner) UVUpgrade(_ []string) (string, error) {
	m.uvUpgradeCalls++
	return m.uvUpgradeOut, m.uvUpgradeErr
}

func (m *mockRunner) UVToolList() ([]string, error) {
	return m.uvToolListResult, m.uvToolListErr
}

func brewfile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Brewfile")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAdd_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "existing"`)
	m := &mockRunner{
		// After DirectInstall, Sync sees both existing and htop as leaves.
		leavesResult:       []string{"existing", "htop"},
		listFormulaeResult: []string{"existing", "htop"},
	}

	result, err := Add(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.directInstallCalls) != 1 {
		t.Fatalf("expected 1 DirectInstall call, got %d", len(m.directInstallCalls))
	}
	if m.directInstallCalls[0].pkg != "htop" {
		t.Errorf("DirectInstall pkg = %q, want %q", m.directInstallCalls[0].pkg, "htop")
	}

	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}

	// Brewfile should now contain htop via Sync.
	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "htop") {
		t.Errorf("Brewfile should contain htop after Sync:\n%s", got)
	}
}

func TestAdd_WithType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{
		listCasksResult: []string{"firefox"},
	}

	_, err := Add(m, bf, "firefox", "cask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.directInstallCalls[0].pkgType != "cask" {
		t.Errorf("DirectInstall pkgType = %q, want %q", m.directInstallCalls[0].pkgType, "cask")
	}
}

func TestAdd_BrewfileUnchangedOnInstallFailure(t *testing.T) {
	t.Parallel()
	original := `brew "existing"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{directInstallErr: errors.New("install failed")}

	_, err := Add(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Brewfile should be untouched — Sync never ran because DirectInstall failed.
	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile should be unchanged.\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestRemove_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "htop"`+"\n"+`brew "curl"`+"\n")
	m := &mockRunner{
		// After DirectUninstall, only curl remains as a leaf.
		leavesResult:       []string{"curl"},
		listFormulaeResult: []string{"curl"},
	}

	result, err := Remove(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.directUninstallCalls) != 1 {
		t.Fatalf("expected 1 DirectUninstall call, got %d", len(m.directUninstallCalls))
	}
	if m.directUninstallCalls[0].pkg != "htop" {
		t.Errorf("DirectUninstall pkg = %q, want %q", m.directUninstallCalls[0].pkg, "htop")
	}
	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}

	// htop should be removed from Brewfile via Sync.
	got, _ := os.ReadFile(bf)
	if strings.Contains(string(got), "htop") {
		t.Errorf("Brewfile should not contain htop after Remove:\n%s", got)
	}
}

func TestRemove_BrewfileUnchangedOnUninstallFailure(t *testing.T) {
	t.Parallel()
	original := `brew "htop"` + "\n" + `brew "curl"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{directUninstallErr: errors.New("uninstall failed")}

	_, err := Remove(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Brewfile should be untouched — Sync never ran because DirectUninstall failed.
	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile should be unchanged.\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestInstall_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "htop"`)
	m := &mockRunner{}

	result, err := Install(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.installCalls) != 1 {
		t.Fatalf("expected 1 install call, got %d", len(m.installCalls))
	}
	if result.Output != "installed" {
		t.Errorf("result.Output = %q, want %q", result.Output, "installed")
	}
}

func TestList_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "htop"`+"\n"+`brew "curl"`)
	m := &mockRunner{listResult: []string{"htop", "curl"}}

	result, err := List(m, bf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result.Packages))
	}
	if result.Packages[0] != "htop" {
		t.Errorf("result.Packages[0] = %q, want %q", result.Packages[0], "htop")
	}
}

func TestList_WithType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{listResult: []string{"firefox"}}

	_, err := List(m, bf, "cask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.listCalls[0].pkgType != "cask" {
		t.Errorf("list pkgType = %q, want %q", m.listCalls[0].pkgType, "cask")
	}
}

func TestParseBrewInfo_Formulae(t *testing.T) {
	t.Parallel()
	jsonData := `{"formulae":[{"name":"htop","desc":"Improved top","versions":{"stable":"3.4.1"}},{"name":"curl","desc":"Get a file from an HTTP server","versions":{"stable":"8.9.1"}}],"casks":[]}`
	pkgs, err := parseBrewInfo(jsonData, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "htop" || pkgs[0].Desc != "Improved top" || pkgs[0].Type != "formula" {
		t.Errorf("pkgs[0] = %+v", pkgs[0])
	}
	if pkgs[0].Version != "3.4.1" {
		t.Errorf("pkgs[0].Version = %q, want %q", pkgs[0].Version, "3.4.1")
	}
}

func TestParseBrewInfo_Casks(t *testing.T) {
	t.Parallel()
	jsonData := `{"formulae":[],"casks":[{"token":"firefox","desc":"Web browser","version":"149.0"}]}`
	pkgs, err := parseBrewInfo(jsonData, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Name != "firefox" || pkgs[0].Type != "cask" {
		t.Errorf("pkgs[0] = %+v", pkgs[0])
	}
	if pkgs[0].Version != "149.0" {
		t.Errorf("pkgs[0].Version = %q, want %q", pkgs[0].Version, "149.0")
	}
}

func TestListDetailed_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "htop"`+"\n"+`cask "firefox"`)
	m := &mockRunner{
		listResult: []string{"htop"},
		infoResult: []PackageInfo{
			{Name: "htop", Desc: "Improved top", Type: "formula"},
		},
	}

	result, err := ListDetailed(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) < 1 {
		t.Fatal("expected at least 1 package")
	}
	if result[0].Name != "htop" || result[0].Desc != "Improved top" {
		t.Errorf("result[0] = %+v", result[0])
	}
}

func TestParseBundleCheck_Satisfied(t *testing.T) {
	t.Parallel()
	out := "The Brewfile's dependencies are satisfied.\n"
	missing := parseBundleCheck(out)
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestParseBundleCheck_Missing(t *testing.T) {
	t.Parallel()
	out := `brew bundle can't satisfy your Brewfile's dependencies.
→ Cask visual-studio-code needs to be installed or updated.
→ Formula git needs to be installed or updated.
→ Formula uv needs to be installed or updated.
Satisfy missing dependencies with ` + "`brew bundle install`.\n"

	missing := parseBundleCheck(out)
	want := []string{"visual-studio-code", "git", "uv"}
	if len(missing) != len(want) {
		t.Fatalf("got %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Errorf("missing[%d] = %q, want %q", i, missing[i], want[i])
		}
	}
}

func TestParseBundleCheck_UVTools(t *testing.T) {
	t.Parallel()
	out := `brew bundle can't satisfy your Brewfile's dependencies.
→ uv Tool symbex needs to be installed.
→ uv Tool sqlite-utils needs to be installed.
→ Formula harper needs to be installed or updated.
Satisfy missing dependencies with ` + "`brew bundle install`.\n"

	missing := parseBundleCheck(out)
	want := []string{"symbex", "sqlite-utils", "harper"}
	if len(missing) != len(want) {
		t.Fatalf("got %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Errorf("missing[%d] = %q, want %q", i, missing[i], want[i])
		}
	}
}

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

func TestParseOutdated(t *testing.T) {
	t.Parallel()
	jsonData := `{"formulae":[{"name":"htop","installed_versions":["3.3.0"],"current_version":"3.4.0","pinned":false}],"casks":[{"name":"firefox","installed_versions":["130.0"],"current_version":"131.0"}]}`
	pkgs, err := parseOutdated(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "htop" || pkgs[0].InstalledVersion != "3.3.0" || pkgs[0].CurrentVersion != "3.4.0" {
		t.Errorf("pkgs[0] = %+v", pkgs[0])
	}
	if pkgs[1].Name != "firefox" || pkgs[1].InstalledVersion != "130.0" || pkgs[1].CurrentVersion != "131.0" {
		t.Errorf("pkgs[1] = %+v", pkgs[1])
	}
}

func TestParseOutdated_Empty(t *testing.T) {
	t.Parallel()
	pkgs, err := parseOutdated("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestParseUVOutdated(t *testing.T) {
	t.Parallel()
	output := `huggingface-hub v0.36.2 [latest: 1.9.2]
- hf
- huggingface-cli
- tiny-agents
marimo v0.22.4 [latest: 0.23.0]
- marimo
scipy v0.1.0 [latest: 1.17.1]
- scipy
`
	pkgs := parseUVOutdated(output)
	want := []OutdatedPackage{
		{Name: "huggingface-hub", InstalledVersion: "0.36.2", CurrentVersion: "1.9.2"},
		{Name: "marimo", InstalledVersion: "0.22.4", CurrentVersion: "0.23.0"},
		{Name: "scipy", InstalledVersion: "0.1.0", CurrentVersion: "1.17.1"},
	}
	if len(pkgs) != len(want) {
		t.Fatalf("got %d packages, want %d", len(pkgs), len(want))
	}
	for i := range want {
		if pkgs[i] != want[i] {
			t.Errorf("pkgs[%d] = %+v, want %+v", i, pkgs[i], want[i])
		}
	}
}

func TestParseUVOutdated_Empty(t *testing.T) {
	t.Parallel()
	pkgs := parseUVOutdated("")
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestParseUVToolList(t *testing.T) {
	t.Parallel()
	output := `huggingface-hub v0.36.2
- hf
- huggingface-cli
- tiny-agents
marimo v0.22.4
- marimo
ruff v0.15.9
- ruff
`
	names := parseUVToolList(output)
	want := []string{"huggingface-hub", "marimo", "ruff"}
	if len(names) != len(want) {
		t.Fatalf("got %d names, want %d", len(names), len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestParseUVToolList_Empty(t *testing.T) {
	t.Parallel()
	names := parseUVToolList("")
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestParseUVToolList_OnlyExecutables(t *testing.T) {
	t.Parallel()
	output := `- marimo
- ruff
`
	names := parseUVToolList(output)
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestParseUVOutdated_NoneOutdated(t *testing.T) {
	t.Parallel()
	// When nothing is outdated, uv outputs tool list without [latest: ...] markers
	output := `marimo v0.22.4
- marimo
ruff v0.15.9
- ruff
`
	pkgs := parseUVOutdated(output)
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestSyncResult_Human_NoChanges(t *testing.T) {
	t.Parallel()
	r := SyncResult{}
	if got := r.Human(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSyncResult_Human_Added(t *testing.T) {
	t.Parallel()
	r := SyncResult{Added: 3, AddedNames: []string{"a", "b", "c"}}
	want := "Synced Brewfile (added 3)\n"
	if got := r.Human(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyncResult_Human_Removed(t *testing.T) {
	t.Parallel()
	r := SyncResult{Removed: 2, RemovedNames: []string{"x", "y"}}
	want := "Synced Brewfile (removed 2)\n"
	if got := r.Human(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyncResult_Human_Both(t *testing.T) {
	t.Parallel()
	r := SyncResult{Added: 3, Removed: 2}
	want := "Synced Brewfile (added 3, removed 2)\n"
	if got := r.Human(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSync_NoChanges(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\n")
	m := &mockRunner{leavesResult: []string{"htop"}, listFormulaeResult: []string{"htop"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Added != 0 || result.Removed != 0 {
		t.Errorf("expected no changes, got added=%d removed=%d", result.Added, result.Removed)
	}
}

func TestSync_AddsBrewEntries(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\n")
	m := &mockRunner{leavesResult: []string{"htop", "curl"}, listFormulaeResult: []string{"htop", "curl"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Added != 1 {
		t.Errorf("expected 1 added, got %d", result.Added)
	}

	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "brew \"curl\"") {
		t.Errorf("Brewfile should contain curl:\n%s", got)
	}
}

func TestSync_AddsUVEntries(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{uvToolListResult: []string{"ruff"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Added != 1 {
		t.Errorf("expected 1 added, got %d", result.Added)
	}

	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "uv \"ruff\"") {
		t.Errorf("Brewfile should contain uv ruff:\n%s", got)
	}
}

func TestSync_RemovesBrewEntries(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\nbrew \"wget\"\n")
	m := &mockRunner{leavesResult: []string{"htop"}, listFormulaeResult: []string{"htop"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", result.Removed)
	}

	got, _ := os.ReadFile(bf)
	if strings.Contains(string(got), "wget") {
		t.Errorf("Brewfile should not contain wget:\n%s", got)
	}
}

func TestSync_RemovesUVEntries(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "uv \"ruff\"\nuv \"marimo\"\n")
	m := &mockRunner{uvToolListResult: []string{"marimo"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", result.Removed)
	}

	got, _ := os.ReadFile(bf)
	if strings.Contains(string(got), "ruff") {
		t.Errorf("Brewfile should not contain ruff:\n%s", got)
	}
	if !strings.Contains(string(got), "marimo") {
		t.Errorf("Brewfile should still contain marimo:\n%s", got)
	}
}

func TestSync_Bidirectional(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\nuv \"ruff\"\n")
	m := &mockRunner{
		leavesResult:       []string{"htop", "curl"},
		listFormulaeResult: []string{"htop", "curl"},
		uvToolListResult:   []string{"marimo"},
	}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Added != 2 {
		t.Errorf("expected 2 added, got %d", result.Added)
	}
	if result.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", result.Removed)
	}
}

func TestSync_LeavesError(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{leavesErr: errors.New("leaves failed")}

	_, err := Sync(m, bf)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSync_UVListError(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{uvToolListErr: errors.New("uv failed")}

	_, err := Sync(m, bf)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSync_IgnoresUnscannableTypes(t *testing.T) {
	t.Parallel()
	// go and cargo entries in Brewfile should not be removed even if not detected
	bf := brewfile(t, "brew \"htop\"\ngo \"github.com/foo/bar\"\ncargo \"ripgrep\"\n")
	m := &mockRunner{leavesResult: []string{"htop"}, listFormulaeResult: []string{"htop"}}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed (go/cargo unscannable), got %d", result.Removed)
	}

	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "go \"github.com/foo/bar\"") {
		t.Errorf("go entry should be preserved:\n%s", got)
	}
}

func TestSync_KeepsDepOnlyFormulae(t *testing.T) {
	t.Parallel()
	// sqlite is in the Brewfile and installed (as a dependency of another
	// formula), but NOT a leaf. Sync should not remove it.
	bf := brewfile(t, "brew \"htop\"\nbrew \"sqlite\"\n")
	m := &mockRunner{
		leavesResult:       []string{"htop"},           // sqlite not a leaf
		listFormulaeResult: []string{"htop", "sqlite"}, // but it IS installed
	}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed (sqlite is installed as dep), got %d", result.Removed)
	}

	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "sqlite") {
		t.Errorf("Brewfile should still contain sqlite:\n%s", got)
	}
}

func TestSync_KeepsTapFormulae(t *testing.T) {
	t.Parallel()
	// Tap formulae like oven-sh/bun/bun appear with full names in both
	// leaves and list --formula --full-name. Sync must match them correctly.
	bf := brewfile(t, "brew \"oven-sh/bun/bun\"\n")
	m := &mockRunner{
		leavesResult:       []string{"oven-sh/bun/bun"},
		listFormulaeResult: []string{"oven-sh/bun/bun"},
	}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed, got %d", result.Removed)
	}
	if result.Added != 0 {
		t.Errorf("expected 0 added, got %d", result.Added)
	}

	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "oven-sh/bun/bun") {
		t.Errorf("Brewfile should still contain oven-sh/bun/bun:\n%s", got)
	}
}

func TestRemoveEntries_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\ncask \"firefox\"\nbrew \"curl\"\nuv \"ruff\"\n")
	toRemove := []BrewfileEntry{
		{Type: "cask", Name: "firefox"},
		{Type: "uv", Name: "ruff"},
	}

	names, err := RemoveEntries(bf, toRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(names))
	}

	got, _ := os.ReadFile(bf)
	want := "brew \"htop\"\nbrew \"curl\"\n"
	if string(got) != want {
		t.Errorf("file content:\ngot:  %q\nwant: %q", string(got), want)
	}
}

func TestRemoveEntries_NoMatch(t *testing.T) {
	t.Parallel()
	original := "brew \"htop\"\nbrew \"curl\"\n"
	bf := brewfile(t, original)
	toRemove := []BrewfileEntry{
		{Type: "cask", Name: "nonexistent"},
	}

	names, err := RemoveEntries(bf, toRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected 0 removed, got %d", len(names))
	}

	got, _ := os.ReadFile(bf)
	if string(got) != original {
		t.Errorf("file should be unchanged:\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestRemoveEntries_PreservesComments(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "# My tools\nbrew \"htop\"\n\nbrew \"curl\"\n# end\n")
	toRemove := []BrewfileEntry{
		{Type: "brew", Name: "curl"},
	}

	_, err := RemoveEntries(bf, toRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(bf)
	want := "# My tools\nbrew \"htop\"\n\n# end\n"
	if string(got) != want {
		t.Errorf("file content:\ngot:  %q\nwant: %q", string(got), want)
	}
}

func TestParseBrewfileEntries_MalformedLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    int // expected number of parsed entries
	}{
		{
			name:    "single word line (no name)",
			content: "brew\n",
			want:    0,
		},
		{
			name:    "empty lines and comments only",
			content: "# comment\n\n# another\n",
			want:    0,
		},
		{
			name:    "unclosed quote still parses name",
			content: `brew "unclosed` + "\n",
			want:    1, // Trim strips quotes from edges
		},
		{
			name:    "extra whitespace",
			content: "  brew   \"git\"  \n",
			want:    1,
		},
		{
			name:    "mixed valid and malformed",
			content: "brew \"git\"\njust-garbage\ncask \"firefox\"\n",
			want:    2, // "just-garbage" has 1 field, skipped
		},
		{
			name:    "version pinned package",
			content: `brew "python@3.12"` + "\n",
			want:    1,
		},
		{
			name:    "bracket extras",
			content: `uv "marimo[recommended]"` + "\n",
			want:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := ParseBrewfileEntries(tt.content)
			if len(entries) != tt.want {
				t.Errorf("got %d entries, want %d; entries=%v", len(entries), tt.want, entries)
			}
		})
	}
}

func TestParseBrewfileEntries_VersionPinned(t *testing.T) {
	t.Parallel()
	entries := ParseBrewfileEntries(`brew "python@3.12"` + "\n")
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Name != "python@3.12" {
		t.Errorf("Name = %q, want %q", entries[0].Name, "python@3.12")
	}
	if entries[0].Type != "brew" {
		t.Errorf("Type = %q, want %q", entries[0].Type, "brew")
	}
}

func TestParseBrewfileEntries_BracketStripped(t *testing.T) {
	t.Parallel()
	entries := ParseBrewfileEntries(`uv "marimo[recommended]"` + "\n")
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Name != "marimo" {
		t.Errorf("Name = %q, want %q (bracket suffix not stripped)", entries[0].Name, "marimo")
	}
}

func TestBrewfileEntry_Label(t *testing.T) {
	t.Parallel()
	e := BrewfileEntry{Type: "brew", Name: "git"}
	if got := e.Label(); got != "git (brew)" {
		t.Errorf("Label() = %q, want %q", got, "git (brew)")
	}
}

// ---------------------------------------------------------------------------
// BundleCheck error-handling tests (real runner uses isBundleCheckOutput)
// ---------------------------------------------------------------------------

func TestIsBundleCheckOutput_MissingDeps(t *testing.T) {
	t.Parallel()
	out := "→ Formula git needs to be installed or updated.\n→ Cask firefox needs to be installed or updated.\n"
	if !isBundleCheckOutput(out) {
		t.Error("expected true for output with missing deps")
	}
}

func TestIsBundleCheckOutput_Satisfied(t *testing.T) {
	t.Parallel()
	out := "The Brewfile's dependencies are satisfied.\n"
	if !isBundleCheckOutput(out) {
		t.Error("expected true for satisfied output")
	}
}

func TestIsBundleCheckOutput_RealError(t *testing.T) {
	t.Parallel()
	out := "Error: No such file or directory @ rb_check_realpath_internal\n"
	if isBundleCheckOutput(out) {
		t.Error("expected false for a real error message")
	}
}

func TestIsBundleCheckOutput_Empty(t *testing.T) {
	t.Parallel()
	if isBundleCheckOutput("") {
		t.Error("expected false for empty output")
	}
}

// ---------------------------------------------------------------------------
// UpgradeOnly tests
// ---------------------------------------------------------------------------

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

func TestSync_AdditionsAreSorted(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	// Sync should sort additions so the Brewfile is deterministic
	// regardless of map iteration order.
	m := &mockRunner{
		leavesResult:       []string{"wget", "curl"},
		listFormulaeResult: []string{"wget", "curl"},
		listCasksResult:    []string{"zed", "alacritty"},
		uvToolListResult:   []string{"ruff", "marimo"},
	}

	_, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(bf)
	entries := ParseBrewfileEntries(string(got))

	// Verify entries are sorted by type then name.
	for i := 1; i < len(entries); i++ {
		prev := entries[i-1].Type + "\t" + entries[i-1].Name
		curr := entries[i].Type + "\t" + entries[i].Name
		if prev > curr {
			t.Errorf("entries not sorted: %q > %q\nfull Brewfile:\n%s", prev, curr, got)
			break
		}
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in, want string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute/path", "/absolute/path"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		got, err := ExpandPath(tt.in)
		if err != nil {
			t.Fatalf("ExpandPath(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
