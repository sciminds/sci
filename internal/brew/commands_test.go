package brew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Compile-time interface assertions.
var (
	_ Runner = BundleRunner{}
	_ Runner = (*mockRunner)(nil)
)

// mockRunner records calls and returns preset results.
type mockRunner struct {
	addCalls     []mockCall
	removeCalls  []mockCall
	installCalls []string
	checkCalls   []string
	cleanupCalls []string
	listCalls    []mockCall

	installErr  error
	checkResult []string
	checkErr    error
	cleanupErr  error
	listResult  []string
	listErr     error
	infoResult  []PackageInfo
	infoErr     error

	updateCalls    int
	updateErr      error
	outdatedResult []OutdatedPackage
	outdatedErr    error
	upgradeCalls   int
	upgradeOut     string
	upgradeErr     error
}

type mockCall struct {
	file, pkg, pkgType string
}

func (m *mockRunner) BundleAdd(file, pkg, pkgType string) error {
	m.addCalls = append(m.addCalls, mockCall{file, pkg, pkgType})
	return nil
}

func (m *mockRunner) BundleRemove(file, pkg, pkgType string) error {
	m.removeCalls = append(m.removeCalls, mockCall{file, pkg, pkgType})
	return nil
}

func (m *mockRunner) BundleInstall(file string) (string, error) {
	m.installCalls = append(m.installCalls, file)
	return "installed", m.installErr
}

func (m *mockRunner) BundleInstallLive(file string, _ func(string), _, _ func()) (string, error) {
	return m.BundleInstall(file)
}

func (m *mockRunner) BundleCheck(file string) ([]string, error) {
	m.checkCalls = append(m.checkCalls, file)
	return m.checkResult, m.checkErr
}

func (m *mockRunner) BundleDump(_ string) error { return nil }

func (m *mockRunner) BundleCleanup(file string) (string, error) {
	m.cleanupCalls = append(m.cleanupCalls, file)
	return "cleaned", m.cleanupErr
}

func (m *mockRunner) BundleList(file, pkgType string) ([]string, error) {
	m.listCalls = append(m.listCalls, mockCall{file: file, pkgType: pkgType})
	return m.listResult, m.listErr
}

func (m *mockRunner) Info(_ []string, _ bool) ([]PackageInfo, error) {
	return m.infoResult, m.infoErr
}

func (m *mockRunner) Update(_ func(string)) error {
	m.updateCalls++
	return m.updateErr
}

func (m *mockRunner) Outdated() ([]OutdatedPackage, error) {
	return m.outdatedResult, m.outdatedErr
}

func (m *mockRunner) Upgrade(_ func(string)) (string, error) {
	m.upgradeCalls++
	return m.upgradeOut, m.upgradeErr
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
	bf := brewfile(t, `brew "existing"`)
	m := &mockRunner{}

	result, err := Add(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Runner was called in correct order: add then install
	if len(m.addCalls) != 1 {
		t.Fatalf("expected 1 add call, got %d", len(m.addCalls))
	}
	if m.addCalls[0].pkg != "htop" {
		t.Errorf("add pkg = %q, want %q", m.addCalls[0].pkg, "htop")
	}
	if len(m.installCalls) != 1 {
		t.Fatalf("expected 1 install call, got %d", len(m.installCalls))
	}

	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}
}

func TestAdd_WithType(t *testing.T) {
	bf := brewfile(t, "")
	m := &mockRunner{}

	_, err := Add(m, bf, "firefox", "cask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.addCalls[0].pkgType != "cask" {
		t.Errorf("add pkgType = %q, want %q", m.addCalls[0].pkgType, "cask")
	}
}

func TestAdd_RollbackOnInstallFailure(t *testing.T) {
	original := `brew "existing"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{installErr: errors.New("install failed")}

	_, err := Add(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Brewfile should be restored to original content
	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile not restored.\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestRemove_HappyPath(t *testing.T) {
	bf := brewfile(t, `brew "htop"`+"\n"+`brew "curl"`+"\n")
	m := &mockRunner{}

	result, err := Remove(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.removeCalls) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(m.removeCalls))
	}
	if m.removeCalls[0].pkg != "htop" {
		t.Errorf("remove pkg = %q, want %q", m.removeCalls[0].pkg, "htop")
	}
	if len(m.cleanupCalls) != 1 {
		t.Fatalf("expected 1 cleanup call, got %d", len(m.cleanupCalls))
	}
	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}
}

func TestRemove_RollbackOnCleanupFailure(t *testing.T) {
	original := `brew "htop"` + "\n" + `brew "curl"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{cleanupErr: errors.New("cleanup failed")}

	_, err := Remove(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile not restored.\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestInstall_HappyPath(t *testing.T) {
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
	out := "The Brewfile's dependencies are satisfied.\n"
	missing := parseBundleCheck(out)
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestParseBundleCheck_Missing(t *testing.T) {
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

func TestUpdate_UpgradesOutdated(t *testing.T) {
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "htop", InstalledVersion: "3.3.0", CurrentVersion: "3.4.0"},
		},
		upgradeOut: "==> Upgrading htop\n",
	}

	result, err := Update(m, false, nil, nil)
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
	m := &mockRunner{
		outdatedResult: []OutdatedPackage{
			{Name: "curl", InstalledVersion: "8.8.0", CurrentVersion: "8.9.0"},
		},
	}

	result, err := Update(m, true, nil, nil)
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
	m := &mockRunner{}

	result, err := Update(m, false, nil, nil)
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

func TestUpdate_UpdateFails(t *testing.T) {
	m := &mockRunner{updateErr: errors.New("network error")}

	_, err := Update(m, false, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if m.upgradeCalls != 0 {
		t.Errorf("should not upgrade when update fails")
	}
}

func TestParseOutdated(t *testing.T) {
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
	pkgs, err := parseOutdated("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestExpandPath(t *testing.T) {
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
