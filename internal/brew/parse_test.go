package brew

import (
	"testing"
)

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
// UpgradeOnly tests
// ---------------------------------------------------------------------------
