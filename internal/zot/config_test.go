package zot

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	cfg := &Config{APIKey: "abc123", UserID: "7654321", DataDir: "/tmp/z"}
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

	if err := SaveConfig(&Config{APIKey: "k", UserID: "1"}); err != nil {
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
	if err := SaveConfig(&Config{APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireConfig(); err == nil {
		t.Error("expected error for missing user_id")
	}
}

func TestLoadConfig_MigratesLegacyLibraryID(t *testing.T) {
	withXDGConfigHome(t)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{
  "api_key": "abc123",
  "library_id": "17450224",
  "data_dir": "/tmp/z"
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
	if cfg.UserID != "17450224" {
		t.Errorf("UserID not migrated: got %q", cfg.UserID)
	}
	if cfg.APIKey != "abc123" {
		t.Errorf("APIKey lost: %q", cfg.APIKey)
	}

	// RequireConfig must now accept it (legacy config → live).
	live, err := RequireConfig()
	if err != nil {
		t.Fatalf("RequireConfig after migration: %v", err)
	}
	if live.UserID != "17450224" {
		t.Errorf("RequireConfig UserID = %q", live.UserID)
	}

	// The file on disk should have been rewritten in the new shape:
	// - include "user_id"
	// - NOT include "library_id"
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(onDisk)
	if !strings.Contains(s, `"user_id"`) {
		t.Errorf("disk config not rewritten with user_id: %s", s)
	}
	if strings.Contains(s, `"library_id"`) {
		t.Errorf("disk config still carries legacy library_id: %s", s)
	}
}

func TestExtractConfig_RoundTrip(t *testing.T) {
	withXDGConfigHome(t)

	cfg := &Config{
		APIKey:  "abc123",
		UserID:  "7654321",
		DataDir: "/tmp/z",
		Extract: ExtractConfig{Runner: RunnerSSH, Host: "mbp", Dir: "/tmp/extracts"},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Extract != cfg.Extract {
		t.Errorf("Extract = %+v, want %+v", loaded.Extract, cfg.Extract)
	}
}

// TestExtractConfig_OmittedFromDisk guards the additive-schema promise: a
// config that never touched the extract section must not grow an "extract"
// key on disk, so student configs stay minimal and hand-readable.
func TestExtractConfig_OmittedFromDisk(t *testing.T) {
	withXDGConfigHome(t)
	if err := SaveConfig(&Config{APIKey: "k", UserID: "1"}); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), `"extract"`) {
		t.Errorf("zero-valued extract section serialized to disk: %s", onDisk)
	}
}

func TestExtractRunner(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ExtractConfig
		env      string
		wantRun  string
		wantHost string
		wantErr  bool
	}{
		{name: "default local", wantRun: RunnerLocal},
		{name: "explicit local", cfg: ExtractConfig{Runner: RunnerLocal}, wantRun: RunnerLocal},
		{name: "ssh with host", cfg: ExtractConfig{Runner: RunnerSSH, Host: "mbp"}, wantRun: RunnerSSH, wantHost: "mbp"},
		{name: "ssh without host", cfg: ExtractConfig{Runner: RunnerSSH}, wantErr: true},
		{name: "unknown runner", cfg: ExtractConfig{Runner: "carrier-pigeon"}, wantErr: true},
		{name: "env forces local over ssh", cfg: ExtractConfig{Runner: RunnerSSH, Host: "mbp"}, env: RunnerLocal, wantRun: RunnerLocal},
		{name: "env invalid", cfg: ExtractConfig{}, env: "warp", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvExtractRunner, tt.env)
			cfg := &Config{Extract: tt.cfg}
			run, host, err := cfg.ExtractRunner()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if run != tt.wantRun || host != tt.wantHost {
				t.Errorf("ExtractRunner = (%q, %q), want (%q, %q)", run, host, tt.wantRun, tt.wantHost)
			}
		})
	}
}

func TestClearConfig(t *testing.T) {
	withXDGConfigHome(t)
	if err := SaveConfig(&Config{APIKey: "k", UserID: "1"}); err != nil {
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

func TestValidateUserID(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"", true},
		{"123", false},
		{"abc", true},
		{"12a3", true},
	}
	for _, tt := range tests {
		err := ValidateUserID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateUserID(%q) err = %v, wantErr %v", tt.id, err, tt.wantErr)
		}
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
