package db

import (
	"fmt"
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

// TestCreateDuckDBRefusesStrayWAL — if a .wal file is left over from a
// crashed duckdb session but the main file is absent, Create should
// surface the mismatch rather than silently producing a file whose
// behaviour depends on the WAL.
func TestCreateDuckDBRefusesStrayWAL(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.duckdb")
	walPath := path + ".wal"
	if err := os.WriteFile(walPath, []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("seed wal: %v", err)
	}
	res, err := Create(path)
	if err == nil {
		t.Fatalf("expected error; got %+v", res)
	}
	if !strings.Contains(err.Error(), ".wal") {
		t.Errorf("error should mention the .wal: %q", err)
	}
	// Main file must not have been created.
	if _, err := os.Stat(path); err == nil {
		t.Error("Create should not produce the .duckdb file when a stray .wal blocks it")
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

func TestMirrorBlockedUnderLimit(t *testing.T) {
	// 50 MB under the default 1 GB cap.
	if err := mirrorBlocked("/tmp/x.duckdb", 50*1024*1024); err != nil {
		t.Errorf("expected no block under cap, got %v", err)
	}
}

func TestMirrorBlockedOverLimit(t *testing.T) {
	// 2 GB over the default 1 GB cap.
	err := mirrorBlocked("/tmp/x.duckdb", 2*1024*1024*1024)
	if err == nil {
		t.Fatal("expected block over cap")
	}
	msg := err.Error()
	for _, want := range []string{"sci db", "SCI_DUCKDB_MIRROR_MAX_MB", "2.1 GB", "1.1 GB"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
}

func TestMirrorBlockedEnvOverride(t *testing.T) {
	// Lower the cap to 10 MB via env; a 50 MB file should now block.
	t.Setenv("SCI_DUCKDB_MIRROR_MAX_MB", "10")
	if err := mirrorBlocked("/tmp/x.duckdb", 50*1024*1024); err == nil {
		t.Error("expected block when env-overridden cap is exceeded")
	}
	// Raise the cap above the default; a 2 GB file should now pass.
	t.Setenv("SCI_DUCKDB_MIRROR_MAX_MB", "5120") // 5 GB
	if err := mirrorBlocked("/tmp/x.duckdb", 2*1024*1024*1024); err != nil {
		t.Errorf("expected pass under raised cap, got %v", err)
	}
}

func TestMirrorBlockedEnvInvalidFallsBack(t *testing.T) {
	t.Setenv("SCI_DUCKDB_MIRROR_MAX_MB", "not-a-number")
	// A 2 GB file should still block under the default 1 GB cap.
	if err := mirrorBlocked("/tmp/x.duckdb", 2*1024*1024*1024); err == nil {
		t.Error("expected block to use default when env is malformed")
	}
}

func TestFormatLossyNoteInline(t *testing.T) {
	cols := []duck.LossyColumn{
		{Table: "events", Column: "payload", Type: "STRUCT(a INTEGER)"},
		{Table: "events", Column: "tags", Type: "VARCHAR[]"},
	}
	got := formatLossyNote(cols)
	if !strings.Contains(got, "2 column") {
		t.Errorf("note %q should mention count", got)
	}
	if !strings.Contains(got, "events.payload") || !strings.Contains(got, "STRUCT") {
		t.Errorf("inline note should name each column: %q", got)
	}
	if !strings.Contains(got, "sci db cols") {
		t.Errorf("note should point at `sci db cols`: %q", got)
	}
}

func TestFormatLossyNoteSummarisesWhenLarge(t *testing.T) {
	cols := make([]duck.LossyColumn, 0, 10)
	for i := 0; i < 4; i++ {
		cols = append(cols, duck.LossyColumn{Table: "a", Column: fmt.Sprintf("c%d", i), Type: "STRUCT"})
	}
	for i := 0; i < 4; i++ {
		cols = append(cols, duck.LossyColumn{Table: "b", Column: fmt.Sprintf("c%d", i), Type: "VARCHAR[]"})
	}
	got := formatLossyNote(cols)
	if !strings.Contains(got, "8 column") {
		t.Errorf("summary note should mention total count: %q", got)
	}
	if !strings.Contains(got, "a: 4") || !strings.Contains(got, "b: 4") {
		t.Errorf("summary should show per-table counts: %q", got)
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
