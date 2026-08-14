package zot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/adrg/xdg"
)

// withXDGConfigHome points xdg.ConfigHome at a fresh temp dir for the test.
func withXDGConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	return dir
}

func TestConfigPath(t *testing.T) {
	dir := withXDGConfigHome(t)
	got := ConfigPath()
	want := filepath.Join(dir, "sci", "zot.json")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

// TestConfigPath_EmptyXDGConfigHome guards against a regression where an
// empty XDG_CONFIG_HOME (set in the shell, not just unset) caused the
// xdg lib to fall back to the macOS-native ~/Library/Application Support
// instead of the ~/.config path users expect. ConfigPath must treat the
// empty case the same as "unset" and route to $HOME/.config.
func TestConfigPath_EmptyXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config", "sci", "zot.json")
	if got := ConfigPath(); got != want {
		t.Errorf("ConfigPath with empty XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}

func TestLoadConfig_Missing(t *testing.T) {
	withXDGConfigHome(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing file, got %+v", cfg)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	withXDGConfigHome(t)

	cfg := &Config{SharedGroupID: "6506098", SharedGroupName: "sciminds", DataDir: "/tmp/z"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected config, got nil")
	}
	if *loaded != *cfg {
		t.Errorf("loaded = %+v, want %+v", loaded, cfg)
	}
}

func TestSaveConfig_Permissions(t *testing.T) {
	withXDGConfigHome(t)

	if err := SaveConfig(&Config{DataDir: "/tmp/z"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
}

func TestRequireConfig_NotConfigured(t *testing.T) {
	withXDGConfigHome(t)
	if _, err := RequireConfig(); err == nil {
		t.Error("expected error for missing config")
	}
}

func TestRequireConfig_IncompleteConfig(t *testing.T) {
	withXDGConfigHome(t)
	if err := SaveConfig(&Config{SharedGroupID: "6506098"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireConfig(); err == nil {
		t.Error("expected error for missing data_dir")
	}
}

// TestLoadConfig_DropsRetiredCredentials is the migration that matters most
// for the credential removal: every existing user's ~/.config/sci/zot.json
// carries `api_key` and `user_id`, and the fields no longer exist. Those keys
// must be dropped SILENTLY on the next load — the config is still complete
// (it names a data directory), so surfacing "zot not configured" would break
// every already-working install for a field nothing reads any more.
//
// Dropped means dropped from what sci LOADS, not scrubbed from the file: the
// disk half is TestLoadConfig_ReadIsNonDestructive below, and it asserts the
// opposite of the tidy-up instinct.
func TestLoadConfig_DropsRetiredCredentials(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := seedDataDir(t)
	legacy := []byte(`{
  "api_key": "zXXXXXXXXXXXXXXXXXXXXXXXX",
  "user_id": "17450224",
  "shared_group_id": "6506098",
  "shared_group_name": "sciminds",
  "data_dir": ` + strconv.Quote(dataDir) + `
}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil for a present config file")
	}
	if cfg.DataDir != dataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dataDir)
	}
	if cfg.SharedGroupID != "6506098" || cfg.SharedGroupName != "sciminds" {
		t.Errorf("shared group lost: %+v", cfg)
	}

	// The whole point: a config carrying only retired credentials is still a
	// configured install.
	live, err := RequireConfig()
	if err != nil {
		t.Fatalf("RequireConfig on a credential-carrying legacy config: %v", err)
	}
	if live.DataDir != dataDir {
		t.Errorf("RequireConfig DataDir = %q, want %q", live.DataDir, dataDir)
	}
}

// TestLoadConfig_MigratesLegacyLibraryID covers the older rename
// (library_id → user_id) reaching a config shape that has neither field: the
// pre-rename file is still readable, its data_dir still lands, and nothing
// asks for a credential.
func TestLoadConfig_MigratesLegacyLibraryID(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := seedDataDir(t)
	legacy := []byte(`{
  "api_key": "abc123",
  "library_id": "17450224",
  "data_dir": ` + strconv.Quote(dataDir) + `
}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig migration failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil for present legacy file")
	}
	if cfg.DataDir != dataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dataDir)
	}
	if _, err := RequireConfig(); err != nil {
		t.Fatalf("RequireConfig after migration: %v", err)
	}
}

// TestLoadConfig_ReadIsNonDestructive is the correction to an instinct that
// looked like hygiene and was actually data loss.
//
// ~/.config/sci/zot.json can hold keys sci never declared, put there by
// whatever else the user points at that path. Some of them are credentials.
// Scrubbing the ones sci does not read would break their owner on nothing more
// than a `sci zot search`, so a read writes nothing at all.
func TestLoadConfig_ReadIsNonDestructive(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{
  "api_key": "zXXXXXXXXXXXXXXXXXXXXXXXX",
  "user_id": "17450224",
  "data_dir": ` + strconv.Quote(seedDataDir(t)) + `,
  "other_tool_key": "someone-elses-key",
  "other_tool_settings": {"runner": "ssh", "host": "elsewhere"}
}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, err := RequireConfig(); err != nil {
		t.Fatalf("RequireConfig: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Errorf("reading the config rewrote it:\n--- before ---\n%s\n--- after ---\n%s", original, after)
	}
}

// TestSaveConfig_PreservesForeignFields is the write half of the same rule.
// `sci zot setup` marshals a Config, which declares three fields — so a plain
// overwrite would delete every key sci does not own. Those three are updated;
// nothing else in the file is touched.
func TestSaveConfig_PreservesForeignFields(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{
  "api_key": "zXXXXXXXXXXXXXXXXXXXXXXXX",
  "data_dir": "/old/place",
  "other_tool_key": "someone-elses-key",
  "other_tool_settings": {"runner": "ssh", "host": "elsewhere"}
}`)
	if err := os.WriteFile(path, existing, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveConfig(&Config{DataDir: "/new/place", SharedGroupID: "6506098", SharedGroupName: "sciminds"}); err != nil {
		t.Fatal(err)
	}

	m := readConfigObject(t, path)
	if m["data_dir"] != "/new/place" {
		t.Errorf("data_dir = %v, want the newly saved value", m["data_dir"])
	}
	if m["shared_group_name"] != "sciminds" {
		t.Errorf("shared_group_name = %v", m["shared_group_name"])
	}
	for _, kept := range []string{"api_key", "other_tool_key", "other_tool_settings"} {
		if _, ok := m[kept]; !ok {
			t.Errorf("SaveConfig destroyed %q, which sci does not own:\n%+v", kept, m)
		}
	}
}

// TestClearConfig_LeavesForeignFields: `setup --logout` means "forget where
// the library is" — there is no credential here to clear. Deleting the whole
// file to do that would take a credential sci never owned with it, a far
// larger act than the one the user asked for.
func TestClearConfig_LeavesForeignFields(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{
  "api_key": "zXXXXXXXXXXXXXXXXXXXXXXXX",
  "data_dir": "/some/place",
  "shared_group_id": "6506098",
  "shared_group_name": "sciminds"
}`)
	if err := os.WriteFile(path, existing, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ClearConfig(); err != nil {
		t.Fatal(err)
	}

	m := readConfigObject(t, path)
	for _, gone := range sciOwnedConfigFields {
		if _, ok := m[gone]; ok {
			t.Errorf("ClearConfig left sci's own field %q behind: %+v", gone, m)
		}
	}
	if _, ok := m["api_key"]; !ok {
		t.Errorf("ClearConfig took another tool's credential with it: %+v", m)
	}
}

// readConfigObject decodes the config file as a bare map so a test can assert
// on keys the Config struct does not declare.
func readConfigObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("config is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

// seedDataDir builds a temp directory holding a zotero.sqlite, which is what
// RequireConfig now treats as "configured".
func seedDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zotero.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestClearConfig(t *testing.T) {
	withXDGConfigHome(t)
	if err := SaveConfig(&Config{DataDir: "/tmp/z"}); err != nil {
		t.Fatal(err)
	}
	if err := ClearConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ConfigPath()); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err = %v", err)
	}
	// Clearing a missing file is a no-op.
	if err := ClearConfig(); err != nil {
		t.Errorf("ClearConfig on missing file: %v", err)
	}
}

// TestDefaultDataDir_FindsCandidatePaths verifies that DefaultDataDir probes
// every supported location (macOS-style ~/Zotero plus Linux native, snap,
// and Flatpak paths). Each sub-test seeds exactly one candidate and asserts
// DefaultDataDir resolves to it.
func TestDefaultDataDir_FindsCandidatePaths(t *testing.T) {
	tests := []struct {
		name string
		rel  []string // path components relative to $HOME
	}{
		{"macOS ~/Zotero", []string{"Zotero"}},
		{"Linux native ~/.zotero/zotero", []string{".zotero", "zotero"}},
		{"Linux snap zotero-snap", []string{"snap", "zotero-snap", "common", "Zotero"}},
		{"Linux Flatpak org.zotero.Zotero", []string{".var", "app", "org.zotero.Zotero", "data", "Zotero"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			parts := slices.Concat([]string{home}, tt.rel)
			dir := filepath.Join(parts...)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "zotero.sqlite"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			got := DefaultDataDir()
			if got != dir {
				t.Errorf("DefaultDataDir = %q, want %q", got, dir)
			}
		})
	}
}

// TestDefaultDataDir_Empty confirms the empty-string sentinel when no
// candidate path exists — callers rely on this to know they should prompt.
func TestDefaultDataDir_Empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := DefaultDataDir(); got != "" {
		t.Errorf("DefaultDataDir = %q, want empty", got)
	}
}

func TestValidateDataDir(t *testing.T) {
	// Empty
	if err := ValidateDataDir(""); err == nil {
		t.Error("expected error for empty path")
	}
	// Missing file
	if err := ValidateDataDir(t.TempDir()); err == nil {
		t.Error("expected error for dir without zotero.sqlite")
	}
	// Happy path
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zotero.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDataDir(dir); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- LibAll scope ---

func TestValidateLibraryScope_AcceptsAll(t *testing.T) {
	if err := ValidateLibraryScope("all"); err != nil {
		t.Errorf("ValidateLibraryScope(all): %v", err)
	}
}

func TestResolve_All(t *testing.T) {
	cfg := &Config{SharedGroupID: "6506098", SharedGroupName: "sciminds"}
	ref, err := cfg.Resolve(LibAll)
	if err != nil {
		t.Fatalf("Resolve(all): %v", err)
	}
	if ref.Scope != LibAll {
		t.Errorf("Scope = %q, want all", ref.Scope)
	}
}

func TestResolve_All_RequiresSharedGroup(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/z"}
	if _, err := cfg.Resolve(LibAll); err == nil {
		t.Fatal("Resolve(all) without a shared group must error, not degrade to personal-only")
	}
}

// TestResolve_Personal_NeedsNoCredential pins the credential removal at the
// resolver: the personal library is `libraries.type='user'` in the local
// database, so nothing about naming it requires a Zotero account ID.
func TestResolve_Personal_NeedsNoCredential(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/z"}
	ref, err := cfg.Resolve(LibPersonal)
	if err != nil {
		t.Fatalf("Resolve(personal) on a credential-free config: %v", err)
	}
	if ref.Scope != LibPersonal {
		t.Errorf("Scope = %q, want personal", ref.Scope)
	}
}
