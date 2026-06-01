package doctor

import (
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
)

func TestOptionalCatalog_AppsReturnsOnlyCasks(t *testing.T) {
	t.Parallel()
	apps := optionalCatalog(true)
	if len(apps) == 0 {
		t.Fatal("expected at least one cask in the optional catalog")
	}
	for _, e := range apps {
		if e.Type != "cask" {
			t.Errorf("apps catalog contains non-cask %q (type %q)", e.Name, e.Type)
		}
	}
	// The full catalog must be a strict superset (it has CLI + Python tools too).
	full := optionalCatalog(false)
	if len(full) <= len(apps) {
		t.Errorf("full catalog (%d) should exceed the apps subset (%d)", len(full), len(apps))
	}
}

func TestResolveOptionalSet_AppsScopesToCasks(t *testing.T) {
	mock := installedSnap() // nothing installed → everything missing
	got, err := ResolveOptionalSet(mock, OptionalFilter{All: true, Apps: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("--apps --all with nothing installed should resolve to every cask")
	}
	for _, e := range got {
		if e.Type != "cask" {
			t.Errorf("--apps resolved a non-cask: %q (type %q)", e.Name, e.Type)
		}
	}
	wantCasks := lo.CountBy(brew.ParseBrewfileEntries(BrewfileOptional), func(e brew.BrewfileEntry) bool {
		return e.Type == "cask"
	})
	if len(got) != wantCasks {
		t.Errorf("got %d casks, want %d (all missing)", len(got), wantCasks)
	}
}

func TestResolveOptionalSet_AppsRejectsNonCaskInclude(t *testing.T) {
	mock := installedSnap()
	// bat is a CLI formula, not an app — scoping to --apps must reject it.
	_, err := ResolveOptionalSet(mock, OptionalFilter{Apps: true, Include: []string{"bat"}})
	if err == nil {
		t.Fatal("expected --apps --include bat to error (bat is not a cask)")
	}
	if !strings.Contains(err.Error(), "bat") {
		t.Errorf("error should name the rejected tool, got: %v", err)
	}
}

func TestListOptionalTools_AppsReturnsOnlyCasks(t *testing.T) {
	mock := installedSnap()
	got, err := ListOptionalTools(mock, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Tools) == 0 {
		t.Fatal("expected casks in the apps listing")
	}
	for _, tool := range got.Tools {
		if tool.Type != "cask" {
			t.Errorf("apps listing contains non-cask %q (type %q)", tool.Name, tool.Type)
		}
	}
}

func TestInstallOptionalTool_AppsRejectsNonCask(t *testing.T) {
	mock := installedSnap()
	_, err := InstallOptionalTool(mock, "bat", "", true)
	if err == nil {
		t.Fatal("expected --apps install of a non-cask to error")
	}
	if !strings.Contains(err.Error(), "bat") {
		t.Errorf("error should name the tool, got: %v", err)
	}
}

func TestInstallOptionalTool_AppsInstallsCask(t *testing.T) {
	mock := installedSnap() // obsidian not installed
	got, err := InstallOptionalTool(mock, "obsidian", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Installed) != 1 || got.Installed[0] != "obsidian" {
		t.Errorf("Installed = %v, want [obsidian]", got.Installed)
	}
	if len(mock.directInstallCalls) != 1 {
		t.Fatalf("expected 1 DirectInstall call, got %d", len(mock.directInstallCalls))
	}
	if pkg, pkgType := mock.directInstallCalls[0][0], mock.directInstallCalls[0][1]; pkg != "obsidian" || pkgType != "cask" {
		t.Errorf("DirectInstall(%q, %q), want (obsidian, cask)", pkg, pkgType)
	}
}
