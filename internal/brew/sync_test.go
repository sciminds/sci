package brew

import (
	"errors"
	"os"
	"strings"
	"testing"
)

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

func TestSync_KeepsTapFormulae_Homebrew6(t *testing.T) {
	t.Parallel()
	// Homebrew 6.x drops tap formulae from `brew leaves` and reports them by
	// bare name in `brew list --formula` (here: "bun", not "oven-sh/bun/bun").
	// Sync must still recognize the tap-qualified Brewfile entry as installed
	// and neither strip nor duplicate it.
	bf := brewfile(t, "brew \"oven-sh/bun/bun\"\n")
	m := &mockRunner{
		leavesResult:       []string{},      // 6.x omits the tap formula
		listFormulaeResult: []string{"bun"}, // reported under its bare name
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

// ---------------------------------------------------------------------------
// SystemSnapshot tests
// ---------------------------------------------------------------------------
