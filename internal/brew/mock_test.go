package brew

import (
	"os"
	"path/filepath"
	"testing"
)

// Compile-time interface assertions.
var (
	_ Runner = CLI{}
	_ Runner = (*mockRunner)(nil)
)

// mockRunner records calls and returns preset results.
type mockRunner struct {
	infoResult     []PackageInfo
	infoCaskResult []PackageInfo
	infoErr        error

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

	// Batch install tracking.
	installFormulaeCalls [][]string
	installFormulaeErr   error
	installCasksCalls    [][]string
	installCasksErr      error
	installUVToolsCalls  [][]string
	installUVToolsErr    error

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
	uvUpgradeArgs    [][]string
	uvUpgradeOut     string
	uvUpgradeErr     error
	uvToolListResult []string
	uvToolListErr    error

	caskAppPathsResult map[string][]string
	caskAppPathsErr    error
	caskAppPathsCalls  [][]string
}

type mockCall struct {
	pkg, pkgType string
}

func (m *mockRunner) Info(_ []string, isCask bool) ([]PackageInfo, error) {
	if isCask && m.infoCaskResult != nil {
		return m.infoCaskResult, m.infoErr
	}
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

func (m *mockRunner) InstallFormulae(names []string) error {
	m.installFormulaeCalls = append(m.installFormulaeCalls, names)
	return m.installFormulaeErr
}

func (m *mockRunner) InstallCasks(names []string) error {
	m.installCasksCalls = append(m.installCasksCalls, names)
	return m.installCasksErr
}

func (m *mockRunner) InstallUVTools(names []string) error {
	m.installUVToolsCalls = append(m.installUVToolsCalls, names)
	return m.installUVToolsErr
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

func (m *mockRunner) UVUpgrade(specs []string) (string, error) {
	m.uvUpgradeCalls++
	m.uvUpgradeArgs = append(m.uvUpgradeArgs, specs)
	return m.uvUpgradeOut, m.uvUpgradeErr
}

func (m *mockRunner) UVToolList() ([]string, error) {
	return m.uvToolListResult, m.uvToolListErr
}

func (m *mockRunner) CaskAppPaths(names []string) (map[string][]string, error) {
	m.caskAppPathsCalls = append(m.caskAppPathsCalls, names)
	return m.caskAppPathsResult, m.caskAppPathsErr
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
