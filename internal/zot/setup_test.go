package zot

import (
	"os"
	"path/filepath"
	"testing"
)

func mkDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zotero.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSetup_Happy(t *testing.T) {
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	res, err := Setup(SetupInput{APIKey: "key", LibraryID: "123", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.LibraryID != "123" || res.DataDir != dir {
		t.Errorf("unexpected result: %+v", res)
	}

	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.APIKey != "key" || cfg.LibraryID != "123" || cfg.DataDir != dir {
		t.Errorf("persisted config mismatch: %+v", cfg)
	}
}

func TestSetup_PersistsOpenAlex(t *testing.T) {
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	_, err := Setup(SetupInput{
		APIKey:         "key",
		LibraryID:      "123",
		DataDir:        dir,
		OpenAlexEmail:  "me@example.com",
		OpenAlexAPIKey: "oa-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.OpenAlexEmail != "me@example.com" {
		t.Errorf("OpenAlexEmail = %q", cfg.OpenAlexEmail)
	}
	if cfg.OpenAlexAPIKey != "oa-secret" {
		t.Errorf("OpenAlexAPIKey = %q", cfg.OpenAlexAPIKey)
	}
}

func TestSetup_OpenAlexOmittedIsOK(t *testing.T) {
	// OpenAlex creds are optional — empty values must not fail validation.
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	_, err := Setup(SetupInput{APIKey: "key", LibraryID: "123", DataDir: dir})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	cfg, _ := LoadConfig()
	if cfg.OpenAlexEmail != "" || cfg.OpenAlexAPIKey != "" {
		t.Errorf("expected empty OpenAlex fields, got %+v", cfg)
	}
}

func TestSetup_InvalidInputs(t *testing.T) {
	dir := mkDataDir(t)
	withXDGConfigHome(t)

	tests := []struct {
		name           string
		key, lib, data string
	}{
		{"empty key", "", "1", dir},
		{"empty lib", "k", "", dir},
		{"non-numeric lib", "k", "abc", dir},
		{"bad dir", "k", "1", filepath.Join(t.TempDir(), "nonexistent")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := SetupInput{APIKey: tt.key, LibraryID: tt.lib, DataDir: tt.data}
			if _, err := Setup(in); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestConfigExists(t *testing.T) {
	withXDGConfigHome(t)
	if ConfigExists() {
		t.Fatal("expected ConfigExists=false before setup")
	}
	dir := mkDataDir(t)
	if _, err := Setup(SetupInput{APIKey: "k", LibraryID: "1", DataDir: dir}); err != nil {
		t.Fatal(err)
	}
	if !ConfigExists() {
		t.Error("expected ConfigExists=true after setup")
	}
	if err := ClearConfig(); err != nil {
		t.Fatal(err)
	}
	if ConfigExists() {
		t.Error("expected ConfigExists=false after ClearConfig")
	}
}

func TestLogout(t *testing.T) {
	withXDGConfigHome(t)
	dir := mkDataDir(t)
	if _, err := Setup(SetupInput{APIKey: "k", LibraryID: "1", DataDir: dir}); err != nil {
		t.Fatal(err)
	}
	res, err := Logout()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Errorf("result: %+v", res)
	}
	if _, err := os.Stat(ConfigPath()); !os.IsNotExist(err) {
		t.Errorf("expected config removed, stat err = %v", err)
	}
}
