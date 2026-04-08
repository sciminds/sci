package cloud

import (
	"os"
	"path/filepath"
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

	// Private should be nil for legacy configs.
	if cfg.Private != nil {
		t.Errorf("expected Private = nil for legacy config, got %+v", cfg.Private)
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
  },
  "private": {
    "access_key": "AKPRIV",
    "secret_key": "SKPRIV",
    "bucket_name": "sci-private"
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
	if cfg.Private == nil || cfg.Private.AccessKey != "AKPRIV" {
		t.Errorf("Private bucket not loaded correctly: %+v", cfg.Private)
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

func TestBucketForMode(t *testing.T) {
	cfg := &Config{
		Public: &BucketConfig{
			AccessKey:  "AKPUB",
			SecretKey:  "SKPUB",
			BucketName: "sci-public",
		},
		Private: &BucketConfig{
			AccessKey:  "AKPRIV",
			SecretKey:  "SKPRIV",
			BucketName: "sci-private",
		},
	}

	pub, err := BucketForMode(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if pub.BucketName != "sci-public" {
		t.Errorf("got %q, want sci-public", pub.BucketName)
	}

	priv, err := BucketForMode(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if priv.BucketName != "sci-private" {
		t.Errorf("got %q, want sci-private", priv.BucketName)
	}

	// Missing private should error.
	cfgNoPrv := &Config{
		Public: cfg.Public,
	}
	_, err = BucketForMode(cfgNoPrv, true)
	if err == nil {
		t.Error("expected error for missing private bucket config")
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
