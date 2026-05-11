package lab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

// withXDGConfigHome points xdg.ConfigHome at a fresh temp dir for the test.
func withXDGConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
}

// TestConfigPath_EmptyXDGConfigHome guards against an empty XDG_CONFIG_HOME
// (set to "" in the shell, not just unset) — without the defensive fallback
// the xdg lib resolves to ~/Library/Application Support on darwin instead
// of ~/.config.
func TestConfigPath_EmptyXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config", "sci", "lab.json")
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

	cfg := &Config{User: "e3jolly"}
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
	if loaded.User != "e3jolly" {
		t.Errorf("User = %q, want %q", loaded.User, "e3jolly")
	}
}

func TestSaveConfig_Permissions(t *testing.T) {
	withXDGConfigHome(t)

	if err := SaveConfig(&Config{User: "alice"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestRequireConfig_NotConfigured(t *testing.T) {
	withXDGConfigHome(t)
	_, err := RequireConfig()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestRequireConfig_EmptyUser(t *testing.T) {
	withXDGConfigHome(t)

	if err := SaveConfig(&Config{User: ""}); err != nil {
		t.Fatal(err)
	}
	_, err := RequireConfig()
	if err == nil {
		t.Fatal("expected error for empty user")
	}
}

func TestRequireConfig_OK(t *testing.T) {
	withXDGConfigHome(t)

	if err := SaveConfig(&Config{User: "e3jolly"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := RequireConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User != "e3jolly" {
		t.Errorf("User = %q, want %q", cfg.User, "e3jolly")
	}
}

func TestConfig_Constants(t *testing.T) {
	if Host != "ssrde.ucsd.edu" {
		t.Errorf("Host = %q", Host)
	}
	if ReadRoot != "/labs/sciminds" {
		t.Errorf("ReadRoot = %q", ReadRoot)
	}
	if WriteRoot != "/labs/sciminds/sci" {
		t.Errorf("WriteRoot = %q", WriteRoot)
	}
}

func TestConfig_WriteDir(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	if got := cfg.WriteDir(); got != "/labs/sciminds/sci/e3jolly" {
		t.Errorf("WriteDir() = %q, want %q", got, "/labs/sciminds/sci/e3jolly")
	}
}

func TestConfig_SSHAlias(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	if got := cfg.SSHAlias(); got != "scilab-e3jolly" {
		t.Errorf("SSHAlias() = %q, want %q", got, "scilab-e3jolly")
	}
}

func TestValidateUser(t *testing.T) {
	tests := []struct {
		user    string
		wantErr bool
	}{
		{"e3jolly", false},
		{"jil605", false},
		{"alice.bob", false},
		{"user_name", false},
		{"user-name", false},
		{"", true},
		{"foo;rm -r /", true},
		{"user name", true},
		{"../etc", true},
		{"-flag", true},
		{"a\nb", true},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			err := ValidateUser(tt.user)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q", tt.user)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tt.user, err)
			}
		})
	}
}
