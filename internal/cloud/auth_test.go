package cloud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

// withXDGConfigHome points xdg.ConfigHome at a fresh temp dir for the test,
// returning the resulting credentials.json path.
func withXDGConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	return filepath.Join(dir, "sci", "credentials.json")
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfig_LegacyMigration(t *testing.T) {
	path := withXDGConfigHome(t)

	legacy := `{
  "account_id": "abc123",
  "access_key": "AKOLD",
  "secret_key": "SKOLD",
  "username": "alice",
  "public_url": "https://pub-xxx.r2.dev",
  "bucket_name": "sci-public"
}`
	writeConfig(t, path, legacy)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.AccountID != "abc123" {
		t.Errorf("AccountID = %q, want %q", cfg.AccountID, "abc123")
	}
	if cfg.Username != "alice" {
		t.Errorf("Username = %q, want %q", cfg.Username, "alice")
	}

	if cfg.Public == nil {
		t.Fatal("expected Public bucket config after migration, got nil")
	}
	if cfg.Public.AccessKey != "AKOLD" {
		t.Errorf("Public.AccessKey = %q, want %q", cfg.Public.AccessKey, "AKOLD")
	}
	if cfg.Public.SecretKey != "SKOLD" {
		t.Errorf("Public.SecretKey = %q, want %q", cfg.Public.SecretKey, "SKOLD")
	}
	if cfg.Public.BucketName != "sci-public" {
		t.Errorf("Public.BucketName = %q, want %q", cfg.Public.BucketName, "sci-public")
	}
	if cfg.Public.PublicURL != "https://pub-xxx.r2.dev" {
		t.Errorf("Public.PublicURL = %q, want %q", cfg.Public.PublicURL, "https://pub-xxx.r2.dev")
	}
}

func TestLoadConfig_NewFormat(t *testing.T) {
	path := withXDGConfigHome(t)

	newFmt := `{
  "username": "bob",
  "github_login": "bob",
  "account_id": "xyz789",
  "public": {
    "access_key": "AKPUB",
    "secret_key": "SKPUB",
    "bucket_name": "sci-public",
    "public_url": "https://pub-xxx.r2.dev"
  }
}`
	writeConfig(t, path, newFmt)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "bob" {
		t.Errorf("Username = %q, want %q", cfg.Username, "bob")
	}
	if cfg.GitHubLogin != "bob" {
		t.Errorf("GitHubLogin = %q, want %q", cfg.GitHubLogin, "bob")
	}
	if cfg.Public == nil || cfg.Public.AccessKey != "AKPUB" {
		t.Errorf("Public bucket not loaded correctly: %+v", cfg.Public)
	}
}

func TestSaveConfig_ClearsLegacyFields(t *testing.T) {
	path := withXDGConfigHome(t)

	cfg := &Config{
		Username:  "carol",
		AccountID: "abc",
		Public: &BucketConfig{
			AccessKey:  "AK",
			SecretKey:  "SK",
			BucketName: "sci-public",
		},
		// Simulate leftover legacy fields.
		LegacyAccessKey: "should-not-persist",
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("saved file is empty")
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LegacyAccessKey != "" {
		t.Error("legacy access_key should not persist after save")
	}
	if loaded.Public == nil || loaded.Public.AccessKey != "AK" {
		t.Errorf("Public bucket not loaded correctly after save: %+v", loaded.Public)
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

func TestLoadConfig_CorruptJSON(t *testing.T) {
	path := withXDGConfigHome(t)
	writeConfig(t, path, "{invalid json!!!")

	cfg, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on parse error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "parse credentials") {
		t.Errorf("error should mention 'parse credentials', got %q", err)
	}
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	path := withXDGConfigHome(t)
	writeConfig(t, path, "")

	cfg, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on parse error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got %q", err)
	}
}

func TestRequireConfig_CorruptFile(t *testing.T) {
	path := withXDGConfigHome(t)
	writeConfig(t, path, "not-json")

	cfg, err := RequireConfig()
	if err == nil {
		t.Fatal("expected error from RequireConfig with corrupt file")
	}
	if cfg != nil {
		t.Error("expected nil config")
	}
}

func TestRequireConfig_IncompleteCredentials(t *testing.T) {
	path := withXDGConfigHome(t)
	writeConfig(t, path, `{"username": "alice"}`)

	cfg, err := RequireConfig()
	if err == nil {
		t.Fatal("expected error for incomplete credentials")
	}
	if cfg != nil {
		t.Error("expected nil config")
	}
	if !strings.Contains(err.Error(), "incomplete credentials") {
		t.Errorf("error should mention 'incomplete credentials', got %q", err)
	}
}

func TestSaveConfig_DirectoryCreationFails(t *testing.T) {
	// Point XDG_CONFIG_HOME at a path whose parent is a regular file, so
	// MkdirAll fails when SaveConfig tries to create $XDG_CONFIG_HOME/sci/.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker, "subdir"))
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	cfg := &Config{Username: "test", AccountID: "id"}
	if err := SaveConfig(cfg); err == nil {
		t.Fatal("expected error when parent is a file, got nil")
	}
}
