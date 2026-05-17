package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/duck"
)

// Phase 2b regression suite — exercises each `sci db` public verb
// against a real .duckdb file. Skips when the duckdb binary is missing,
// the same way internal/duck tests do.

func requireDuck(t *testing.T) {
	t.Helper()
	if !duck.Available() {
		t.Skip("duckdb binary not on PATH; install via `sci doctor` to run this test")
	}
}

// writeTiny is the canonical 3-row CSV used across these tests.
func writeTiny(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "tiny.csv")
	if err := os.WriteFile(path, []byte("id,name,score\n1,alice,3.14\n2,bob,2.72\n3,carol,1.41\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

func TestCreateDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tables) != 0 {
		t.Errorf("expected empty db, got %+v", info.Tables)
	}
}

func TestResetDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rst.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := duck.ImportCSV(path, []string{writeTiny(t, dir)}, "seeded"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Reset(path); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tables) != 0 {
		t.Errorf("expected empty after Reset, got %+v", info.Tables)
	}
}

func TestAddCSVDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "add.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := AddCSV([]string{writeTiny(t, dir)}, path, "people"); err != nil {
		t.Fatalf("AddCSV: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tables) != 1 || info.Tables[0].Name != "people" || info.Tables[0].Rows != 3 {
		t.Errorf("got %+v, want one table people/3", info.Tables)
	}
}

func TestAddCSVDuckDBCollisionMentionsAppend(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "add.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	csv := writeTiny(t, dir)
	if _, err := AddCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("seed AddCSV: %v", err)
	}
	_, err := AddCSV([]string{csv}, path, "people")
	if err == nil {
		t.Fatal("expected collision error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already exists") {
		t.Errorf("error %q does not mention 'already exists'", msg)
	}
	if !strings.Contains(msg, "sci db append") {
		t.Errorf("error %q should suggest `sci db append`", msg)
	}
}

func TestAppendCSVDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "app.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	csv := writeTiny(t, dir)
	if _, err := AddCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := AppendCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("AppendCSV: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Tables[0].Rows != 6 {
		t.Errorf("rows = %d, want 6", info.Tables[0].Rows)
	}
}

func TestDeleteTableDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "del.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := AddCSV([]string{writeTiny(t, dir)}, path, "victim"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := DeleteTable("victim", path); err != nil {
		t.Fatalf("DeleteTable: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tables) != 0 {
		t.Errorf("expected empty after delete, got %+v", info.Tables)
	}
}

func TestRenameTableDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "ren.duckdb")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := AddCSV([]string{writeTiny(t, dir)}, path, "before"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := RenameTable("before", "after", path); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tables) != 1 || info.Tables[0].Name != "after" {
		t.Errorf("got %+v, want one table 'after'", info.Tables)
	}
}

// TestAddCSVSQLiteCollisionMentionsAppend — same wrapping for the SQLite path.
func TestAddCSVSQLiteCollisionMentionsAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "add.db")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	csv := writeTiny(t, dir)
	if _, err := AddCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("seed AddCSV: %v", err)
	}
	_, err := AddCSV([]string{csv}, path, "people")
	if err == nil {
		t.Fatal("expected collision error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already exists") {
		t.Errorf("error %q does not mention 'already exists'", msg)
	}
	if !strings.Contains(msg, "sci db append") {
		t.Errorf("error %q should suggest `sci db append`", msg)
	}
}

// TestAppendCSVSQLite — append works on plain SQLite too.
func TestAppendCSVSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	csv := writeTiny(t, dir)
	if _, err := AddCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := AppendCSV([]string{csv}, path, "people"); err != nil {
		t.Fatalf("AppendCSV: %v", err)
	}
	info, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Tables[0].Rows != 6 {
		t.Errorf("rows = %d, want 6", info.Tables[0].Rows)
	}
}
