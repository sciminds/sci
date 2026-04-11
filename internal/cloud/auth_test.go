package cloud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_LegacyMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	// Write old flat-format credentials.
	legacy := `{
  "account_id": "abc123",
  "access_key": "AKOLD",
  "secret_key": "SKOLD",
  "username": "alice",
  "public_url": "https://pub-xxx.r2.dev",
  "bucket_name": "sci-public"
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	// Top-level fields should parse directly.
	if cfg.AccountID != "abc123" {
		t.Errorf("AccountID = %q, want %q", cfg.AccountID, "abc123")
	}
	if cfg.Username != "alice" {
		t.Errorf("Username = %q, want %q", cfg.Username, "alice")
	}

	// Legacy fields should be migrated into Public bucket.
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

	// Board bucket should also be backfilled so users upgrading from a
	// pre-board-feature version don't need to re-auth.
	if cfg.Board == nil {
		t.Fatal("expected Board bucket config after migration, got nil")
	}
	if cfg.Board.AccessKey != "AKOLD" || cfg.Board.SecretKey != "SKOLD" {
		t.Errorf("Board keys not copied from Public: %+v", cfg.Board)
	}
	if cfg.Board.BucketName != "sci-board" {
		t.Errorf("Board.BucketName = %q, want %q", cfg.Board.BucketName, "sci-board")
	}

}

func TestLoadConfig_NewFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

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
	if err := os.WriteFile(path, []byte(newFmt), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

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
	// New-format file without a board block should auto-backfill one so
	// existing users don't have to re-authenticate on upgrade.
	if cfg.Board == nil || cfg.Board.AccessKey != "AKPUB" || cfg.Board.BucketName != "sci-board" {
		t.Errorf("Board bucket not backfilled: %+v", cfg.Board)
	}
}

func TestSaveConfig_ClearsLegacyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	t.Setenv("SCI_CONFIG_PATH", path)

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

	// Read it back raw to verify legacy fields are absent.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if got := raw; len(got) == 0 {
		t.Fatal("saved file is empty")
	}

	// Reload and verify.
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
	t.Setenv("SCI_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing file, got %+v", cfg)
	}
}

func TestLoadConfig_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte("{invalid json!!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

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
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

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
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

	cfg, err := RequireConfig()
	if err == nil {
		t.Fatal("expected error from RequireConfig with corrupt file")
	}
	if cfg != nil {
		t.Error("expected nil config")
	}
}

func TestRequireConfig_IncompleteCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	// Valid JSON but missing required fields.
	incomplete := `{"username": "alice"}`
	if err := os.WriteFile(path, []byte(incomplete), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCI_CONFIG_PATH", path)

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
	dir := t.TempDir()
	// Create a regular file where MkdirAll needs a directory.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "subdir", "creds.json")
	t.Setenv("SCI_CONFIG_PATH", path)

	cfg := &Config{Username: "test", AccountID: "id"}
	err := SaveConfig(cfg)
	if err == nil {
		t.Fatal("expected error when parent is a file, got nil")
	}
}
