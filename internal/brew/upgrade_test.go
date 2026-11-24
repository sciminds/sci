package brew

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// dockerTwice is a formula and a cask sharing a name.
func dockerTwice() []OutdatedPackage {
	return []OutdatedPackage{
		{Type: TypeFormula, Name: "docker", InstalledVersion: "1", CurrentVersion: "2"},
		{Type: TypeCask, Name: "docker", InstalledVersion: "1", CurrentVersion: "2"},
	}
}

func assertUpgrades(t *testing.T, m *mockRunner, formulae, casks [][]string) {
	t.Helper()
	if !reflect.DeepEqual(m.formulaUpgrades, formulae) {
		t.Errorf("formula upgrades = %v, want %v", m.formulaUpgrades, formulae)
	}
	if !reflect.DeepEqual(m.caskUpgrades, casks) {
		t.Errorf("cask upgrades = %v, want %v", m.caskUpgrades, casks)
	}
}

func TestUpdate_UpgradesByName(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: dockerTwice()}
	if _, err := Update(m, false); err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, [][]string{{"docker"}}, [][]string{{"docker"}})
}

func TestUpdate_SkipsPinned(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: []OutdatedPackage{
		{Type: TypeFormula, Name: "jq", Pinned: true},
		{Type: TypeFormula, Name: "fd"},
	}}
	result, err := Update(m, false)
	if err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, [][]string{{"fd"}}, nil)
	if len(result.Outdated) != 1 || result.Outdated[0].Name != "fd" {
		t.Errorf("Outdated = %+v, want fd only", result.Outdated)
	}
}

func TestUpdate_AllPinned(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: []OutdatedPackage{{Type: TypeFormula, Name: "jq", Pinned: true}}}
	if _, err := Update(m, false); err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, nil, nil)
	if m.uvUpgradeCalls != 0 {
		t.Errorf("uv upgrade calls = %d, want 0", m.uvUpgradeCalls)
	}
}

func TestUpgradeBrew_FormulaFailureStillUpgradesCasks(t *testing.T) {
	t.Parallel()
	m := &mockRunner{formulaUpgradeErr: errors.New("build failed")}
	_, err := UpgradeBrew(m, dockerTwice())
	if err == nil || !strings.Contains(err.Error(), "brew upgrade") {
		t.Errorf("err = %v, want it to mention brew upgrade", err)
	}
	assertUpgrades(t, m, [][]string{{"docker"}}, [][]string{{"docker"}})
}
