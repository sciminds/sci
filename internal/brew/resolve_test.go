package brew

import (
	"errors"
	"slices"
	"testing"
)

func TestUVUpgradeArgs(t *testing.T) {
	t.Parallel()
	// The upgrade must use `--upgrade` (lifts pins, backtracks to the newest
	// installable version) rather than the old `<spec>@latest` form, which
	// hard-pinned to the highest published version and errored out when that
	// version was unsatisfiable under the stable resolver.
	got := uvUpgradeArgs("markitdown[all]")
	want := []string{"tool", "install", "markitdown[all]", "--upgrade"}
	if !slices.Equal(got, want) {
		t.Errorf("uvUpgradeArgs = %v, want %v", got, want)
	}
}

func TestParseResolvedVersion(t *testing.T) {
	t.Parallel()
	output := "azure-ai-contentunderstanding==1.2.0b1\nmarkitdown==0.1.5\nbeautifulsoup4==4.13.0\n"
	tests := []struct {
		pkg  string
		want string
	}{
		{"markitdown", "0.1.5"},
		{"Markitdown", "0.1.5"},                      // case-insensitive
		{"azure_ai_contentunderstanding", "1.2.0b1"}, // separator-normalized
		{"missing", ""},
	}
	for _, tt := range tests {
		if got := parseResolvedVersion(output, tt.pkg); got != tt.want {
			t.Errorf("parseResolvedVersion(%q) = %q, want %q", tt.pkg, got, tt.want)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		candidate, installed string
		want                 bool
	}{
		{"0.1.6", "0.1.5", true},
		{"0.1.5", "0.1.5", false}, // the held-back case: same version
		{"0.1.4", "0.1.5", false},
		{"1.2.0b1", "1.1.0", true},
		{"weird", "weird", false},    // unparseable, equal
		{"weird-a", "weird-b", true}, // unparseable, differ → don't hide
	}
	for _, tt := range tests {
		if got := newerVersion(tt.candidate, tt.installed); got != tt.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tt.candidate, tt.installed, got, tt.want)
		}
	}
}

func TestFilterUVUpgradable(t *testing.T) {
	t.Parallel()
	candidates := []OutdatedPackage{
		{Name: "markitdown", InstalledVersion: "0.1.5", CurrentVersion: "0.1.6"},  // held back
		{Name: "datasette", InstalledVersion: "0.64.0", CurrentVersion: "0.65.0"}, // real upgrade
		{Name: "broken", InstalledVersion: "1.0.0", CurrentVersion: "2.0.0"},      // unresolvable
		{Name: "weird", InstalledVersion: "1.0.0", CurrentVersion: "1.1.0"},       // resolve parse-miss
	}
	// resolve mimics the stable resolver: markitdown can't reach 0.1.6 (backtracks
	// to the installed 0.1.5), datasette resolves to a genuinely newer version,
	// broken can't be resolved at all, and weird's primary version can't be parsed.
	resolve := func(_, pkgName string) (string, error) {
		switch pkgName {
		case "markitdown":
			return "0.1.5", nil
		case "datasette":
			return "0.65.0", nil
		case "broken":
			return "", errors.New("no solution found")
		default:
			return "", nil
		}
	}

	got := filterUVUpgradable(candidates, resolve)

	names := make([]string, len(got))
	for i, p := range got {
		names[i] = p.Name
	}
	wantNames := []string{"datasette", "weird"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("kept %v, want %v", names, wantNames)
	}
	// CurrentVersion for a kept upgrade is the resolved (installable) version.
	if got[0].CurrentVersion != "0.65.0" {
		t.Errorf("datasette CurrentVersion = %q, want 0.65.0", got[0].CurrentVersion)
	}
}

func TestFilterUVUpgradable_Empty(t *testing.T) {
	t.Parallel()
	resolve := func(_, _ string) (string, error) { return "", nil }
	if got := filterUVUpgradable(nil, resolve); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
