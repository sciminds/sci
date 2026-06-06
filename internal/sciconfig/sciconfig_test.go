package sciconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

// withXDGConfigHome points xdg.ConfigHome at a fresh temp dir for the test,
// mirroring the helper used in the per-domain config tests.
func withXDGConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
}

type testConfig struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TestDir_EmptyXDGConfigHome guards the defensive fallback: an empty (not
// unset) XDG_CONFIG_HOME must resolve to ~/.config/sci, not the darwin
// xdg.ConfigHome default (~/Library/Application Support).
func TestDir_EmptyXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config", "sci")
	if got := Dir(); got != want {
		t.Errorf("Dir() with empty XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}

func TestPath(t *testing.T) {
	withXDGConfigHome(t)
	want := filepath.Join(Dir(), "lab.json")
	if got := Path("lab.json"); got != want {
		t.Errorf("Path(lab.json) = %q, want %q", got, want)
	}
	f := File[testConfig]{Name: "thing.json"}
	if got := f.Path(); got != filepath.Join(Dir(), "thing.json") {
		t.Errorf("File.Path() = %q, want %q", got, filepath.Join(Dir(), "thing.json"))
	}
}

func TestLoad_Missing(t *testing.T) {
	withXDGConfigHome(t)
	f := File[testConfig]{Name: "missing.json"}

	cfg, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing file, got %+v", cfg)
	}
	if f.Exists() {
		t.Error("Exists() should be false for missing file")
	}

	raw, err := f.LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if raw != nil {
		t.Errorf("LoadRaw should return nil bytes for missing file, got %q", raw)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	withXDGConfigHome(t)
	f := File[testConfig]{Name: "thing.json"}

	if err := f.Save(&testConfig{Name: "alice", Count: 3}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !f.Exists() {
		t.Error("Exists() should be true after Save")
	}

	loaded, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected config, got nil")
	}
	if loaded.Name != "alice" || loaded.Count != 3 {
		t.Errorf("round-trip = %+v, want {alice 3}", *loaded)
	}
}

func TestSave_Permissions(t *testing.T) {
	withXDGConfigHome(t)
	f := File[testConfig]{Name: "thing.json"}

	if err := f.Save(&testConfig{Name: "bob"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestClear(t *testing.T) {
	withXDGConfigHome(t)
	f := File[testConfig]{Name: "thing.json"}

	// Clearing a missing file is a no-op (no error).
	if err := f.Clear(); err != nil {
		t.Fatalf("Clear on missing file: %v", err)
	}

	if err := f.Save(&testConfig{Name: "carol"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := f.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if f.Exists() {
		t.Error("Exists() should be false after Clear")
	}
}
