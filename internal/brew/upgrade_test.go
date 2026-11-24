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
	if _, err := Update(m, nil, false); err != nil {
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
	result, err := Update(m, nil, false)
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
	if _, err := Update(m, nil, false); err != nil {
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

func TestUpdate_SkipsIgnored(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: dockerTwice()}
	result, err := Update(m, IgnoreList{{Type: TypeCask, Name: "docker"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, [][]string{{"docker"}}, nil)
	if len(result.Ignored) != 1 || result.Ignored[0].Type != TypeCask {
		t.Errorf("Ignored = %+v, want the docker cask", result.Ignored)
	}
}

func TestUpdate_SkipsIgnoredUV(t *testing.T) {
	t.Parallel()
	m := &mockRunner{uvOutdatedResult: []OutdatedPackage{{Type: TypeUV, Name: "ruff"}}}
	result, err := Update(m, IgnoreList{{Type: TypeUV, Name: "ruff"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.uvUpgradeCalls != 0 {
		t.Errorf("uv upgrade calls = %d, want 0", m.uvUpgradeCalls)
	}
	if len(result.Ignored) != 1 {
		t.Errorf("Ignored = %+v, want ruff", result.Ignored)
	}
}

func TestUpdate_AllIgnored(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: dockerTwice()}
	ignored := IgnoreList{{Type: TypeCask, Name: "docker"}, {Type: TypeFormula, Name: "docker"}}
	result, err := Update(m, ignored, false)
	if err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, nil, nil)
	if len(result.Outdated) != 0 || len(result.Ignored) != 2 {
		t.Errorf("result = %+v, want nothing outdated and two ignored", result)
	}
}

func TestUpdate_IgnoredTapFormula(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: []OutdatedPackage{{Type: TypeFormula, Name: "oven-sh/bun/bun"}}}
	if _, err := Update(m, IgnoreList{{Type: TypeFormula, Name: "bun"}}, false); err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, nil, nil)
}

func TestUpgradeOnly_SkipsIgnored(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: dockerTwice()}
	result, err := UpgradeOnly(m, IgnoreList{{Type: TypeFormula, Name: "docker"}})
	if err != nil {
		t.Fatal(err)
	}
	assertUpgrades(t, m, nil, [][]string{{"docker"}})
	if len(result.Ignored) != 1 {
		t.Errorf("Ignored = %+v, want the docker formula", result.Ignored)
	}
}

func TestCheckUpdates_SplitsIgnored(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: dockerTwice()}
	check, err := CheckUpdates(t.Context(), m, IgnoreList{{Type: TypeCask, Name: "docker"}})
	if err != nil {
		t.Fatal(err)
	}
	if all := check.All(); len(all) != 1 || all[0].Type != TypeFormula {
		t.Errorf("All() = %+v, want the docker formula", all)
	}
	if len(check.Ignored) != 1 || check.Ignored[0].Type != TypeCask {
		t.Errorf("Ignored = %+v, want the docker cask", check.Ignored)
	}
}

func TestUpdate_IgnoredAndPinned(t *testing.T) {
	t.Parallel()
	m := &mockRunner{outdatedResult: []OutdatedPackage{{Type: TypeFormula, Name: "jq", Pinned: true}}}
	result, err := Update(m, IgnoreList{{Type: TypeFormula, Name: "jq"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ignored) != 1 || len(result.Pinned) != 0 {
		t.Errorf("Ignored = %+v, Pinned = %+v, want jq ignored only", result.Ignored, result.Pinned)
	}
}
