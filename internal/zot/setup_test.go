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

	res, err := Setup(SetupInput{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.DataDir != dir {
		t.Errorf("unexpected result: %+v", res)
	}

	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.DataDir != dir {
		t.Errorf("persisted config mismatch: %+v", cfg)
	}
}

// The data directory is the only input left, so it is the only one that can
// be invalid — and "invalid" means specifically "holds no zotero.sqlite",
// since that file is the entire configuration.
func TestSetup_InvalidInputs(t *testing.T) {
	withXDGConfigHome(t)

	tests := []struct {
		name string
		data string
	}{
		{"empty dir", ""},
		{"nonexistent dir", filepath.Join(t.TempDir(), "nonexistent")},
		{"dir without zotero.sqlite", t.TempDir()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Setup(SetupInput{DataDir: tt.data}); err == nil {
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
	if _, err := Setup(SetupInput{DataDir: dir}); err != nil {
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
	if _, err := Setup(SetupInput{DataDir: dir}); err != nil {
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
