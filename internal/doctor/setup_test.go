package doctor

import "testing"

func TestParseBrewfileEntries(t *testing.T) {
	content := `brew "helix"
brew "nvim"
cask "visual-studio-code"
uv "symbex"
uv "markitdown[all]"
uv "huggingface-hub", with: ["pillow"]
# a comment

`
	entries := parseBrewfileEntries(content)

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
	entries := parseBrewfileEntries("")
	if len(entries) != 0 {
		t.Errorf("expected empty, got %v", entries)
	}
}

func TestBrewfileEntry_Label(t *testing.T) {
	e := BrewfileEntry{Type: "uv", Name: "symbex"}
	if got := e.Label(); got != "symbex (uv)" {
		t.Errorf("Label() = %q, want %q", got, "symbex (uv)")
	}
}

func TestBrewfileOptionalEmbedded(t *testing.T) {
	if BrewfileOptional == "" {
		t.Fatal("embedded BrewfileOptional is empty")
	}
	entries := parseBrewfileEntries(BrewfileOptional)
	if len(entries) == 0 {
		t.Fatal("embedded BrewfileOptional has no entries")
	}
}
