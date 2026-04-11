package zot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath_Override(t *testing.T) {
	// Explicit override wins over everything.
	t.Setenv("SCI_ZOT_CONFIG_PATH", "/explicit/zot.json")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	p, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/explicit/zot.json" {
		t.Errorf("ConfigPath = %q, want /explicit/zot.json", p)
	}
}

func TestConfigPath_XDG(t *testing.T) {
	t.Setenv("SCI_ZOT_CONFIG_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	p, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join("/xdg", "sci", "zot.json") {
		t.Errorf("ConfigPath = %q, want /xdg/sci/zot.json", p)
	}
}

func TestConfigPath_HomeFallback(t *testing.T) {
	t.Setenv("SCI_ZOT_CONFIG_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/fake/home")
	p, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join("/fake/home", ".config", "sci", "zot.json") {
		t.Errorf("ConfigPath = %q, want /fake/home/.config/sci/zot.json", p)
	}
}

func TestLoadConfig_Missing(t *testing.T) {
	t.Setenv("SCI_ZOT_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing file, got %+v", cfg)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zot.json")
	t.Setenv("SCI_ZOT_CONFIG_PATH", path)

	cfg := &Config{APIKey: "abc123", LibraryID: "7654321", DataDir: "/tmp/z"}
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
	path := filepath.Join(t.TempDir(), "zot.json")
	t.Setenv("SCI_ZOT_CONFIG_PATH", path)

	if err := SaveConfig(&Config{APIKey: "k", LibraryID: "1"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
}

func TestRequireConfig_NotConfigured(t *testing.T) {
	t.Setenv("SCI_ZOT_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.json"))
	if _, err := RequireConfig(); err == nil {
		t.Error("expected error for missing config")
	}
}

func TestRequireConfig_IncompleteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zot.json")
	t.Setenv("SCI_ZOT_CONFIG_PATH", path)
	if err := SaveConfig(&Config{APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireConfig(); err == nil {
		t.Error("expected error for missing library_id")
	}
}

func TestClearConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zot.json")
	t.Setenv("SCI_ZOT_CONFIG_PATH", path)
	if err := SaveConfig(&Config{APIKey: "k", LibraryID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := ClearConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err = %v", err)
	}
	// Clearing a missing file is a no-op.
	if err := ClearConfig(); err != nil {
		t.Errorf("ClearConfig on missing file: %v", err)
	}
}

func TestValidateLibraryID(t *testing.T) {
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
		err := ValidateLibraryID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateLibraryID(%q) err = %v, wantErr %v", tt.id, err, tt.wantErr)
		}
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
