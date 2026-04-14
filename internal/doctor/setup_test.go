package doctor

import (
	"fmt"
	"testing"

	"github.com/sciminds/cli/internal/brew"
)

func TestParseBrewfileEntries(t *testing.T) {
	content := `brew "helix"
brew "nvim"
cask "visual-studio-code"
uv "symbex"
uv "markitdown[all]"
uv "huggingface-hub", with: ["pillow"]
# a comment

`
	entries := brew.ParseBrewfileEntries(content)

	want := []struct {
		typ, name string
	}{
		{"brew", "helix"},
		{"brew", "nvim"},
		{"cask", "visual-studio-code"},
		{"uv", "symbex"},
		{"uv", "markitdown"},
		{"uv", "huggingface-hub"},
	}

	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].Type != w.typ {
			t.Errorf("entries[%d].Type = %q, want %q", i, entries[i].Type, w.typ)
		}
		if entries[i].Name != w.name {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w.name)
		}
		if entries[i].Line == "" {
			t.Errorf("entries[%d].Line is empty", i)
		}
	}
}

func TestParseBrewfileEntries_Empty(t *testing.T) {
	entries := brew.ParseBrewfileEntries("")
	if len(entries) != 0 {
		t.Errorf("expected empty, got %v", entries)
	}
}

func TestBrewfileEntry_Label(t *testing.T) {
	e := brew.BrewfileEntry{Type: "uv", Name: "symbex"}
	if got := e.Label(); got != "symbex (uv)" {
		t.Errorf("Label() = %q, want %q", got, "symbex (uv)")
	}
}

func TestMissingSet_SnapshotError(t *testing.T) {
	// When CollectSnapshot fails, missingSet should return ALL names as missing
	// (not nil/empty, which would hide the problem).
	mock := &mockBrewRunner{
		listFormulaeErr: fmt.Errorf("brew borked"),
	}

	content := `brew "git"
brew "uv"
cask "firefox"
`
	set := missingSet(mock, content)

	if set == nil {
		t.Fatal("missingSet should not return nil on error")
	}
	// All 3 packages should be in the set (treated as missing).
	for _, name := range []string{"git", "uv", "firefox"} {
		if !set[name] {
			t.Errorf("expected %q to be in missing set on snapshot error", name)
		}
	}
}

func TestBrewfileOptionalEmbedded(t *testing.T) {
	if BrewfileOptional == "" {
		t.Fatal("embedded BrewfileOptional is empty")
	}
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	if len(entries) == 0 {
		t.Fatal("embedded BrewfileOptional has no entries")
	}
}
