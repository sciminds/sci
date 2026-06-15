package duck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 1 tests for duckdb write primitives. All shell-out through the
// duckdb binary; the suite skips when it's not on PATH via requireDuck.

func TestCreateEmpty(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	entries, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d tables in empty db, want 0: %+v", len(entries), entries)
	}
}

func TestCreateEmptyRefusesExisting(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("first CreateEmpty: %v", err)
	}
	err := CreateEmpty(path)
	if err == nil {
		t.Fatal("expected CreateEmpty to refuse an existing path")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q does not mention 'already exists'", err)
	}
}

func TestReset(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "reset.duckdb")
	// Seed with one table.
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, ""); err != nil {
		t.Fatalf("seed ImportCSV: %v", err)
	}

	if err := Reset(path); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	entries, err := Info(path)
	if err != nil {
		t.Fatalf("Info post-Reset: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d tables after Reset, want 0: %+v", len(entries), entries)
	}
}

func TestResetOnMissingFile(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "ghost.duckdb")
	// Reset must be idempotent: missing → fresh empty file.
	if err := Reset(path); err != nil {
		t.Fatalf("Reset on missing: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Reset did not create file: %v", err)
	}
}

func TestDropTable(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "drop.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "victim"); err != nil {
		t.Fatalf("seed ImportCSV: %v", err)
	}

	if err := DropTable(path, "victim"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}
	entries, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no tables after drop, got %+v", entries)
	}
}

func TestDropTableMissing(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "drop.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if err := DropTable(path, "ghost"); err == nil {
		t.Fatal("expected error dropping missing table")
	}
}

func TestRenameTable(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rename.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "before"); err != nil {
		t.Fatalf("seed ImportCSV: %v", err)
	}
	if err := RenameTable(path, "before", "after"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}
	entries, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "after" {
		t.Errorf("got %+v, want one table named 'after'", entries)
	}
}

func TestRenameTableCollision(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rename.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "first"); err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "second"); err != nil {
		t.Fatalf("seed second: %v", err)
	}
	if err := RenameTable(path, "first", "second"); err == nil {
		t.Fatal("expected error renaming onto an existing table")
	}
}

func TestImportCSVSingle(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	got, err := ImportCSV(path, []string{tinyCSV}, "")
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(got) != 1 || got[0].Table != "tiny" || got[0].Rows != 3 {
		t.Errorf("got %+v, want one entry tiny/3", got)
	}
}

func TestImportCSVWithTableOverride(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	got, err := ImportCSV(path, []string{tinyCSV}, "people")
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(got) != 1 || got[0].Table != "people" {
		t.Errorf("got %+v, want table=people", got)
	}
}

func TestImportCSVMultiple(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	// Two CSVs in different basenames so default naming yields distinct tables.
	csvA := filepath.Join(dir, "alpha.csv")
	csvB := filepath.Join(dir, "beta.csv")
	for _, p := range []string{csvA, csvB} {
		if err := os.WriteFile(p, []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	got, err := ImportCSV(path, []string{csvA, csvB}, "")
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
}

func TestImportCSVCollisionErrors(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "people"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := ImportCSV(path, []string{tinyCSV}, "people")
	if err == nil {
		t.Fatal("expected error on table collision")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q does not mention 'already exists'", err)
	}
}

func TestImportCSVRejectsUnsafeTable(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "evil; DROP"); err == nil {
		t.Fatal("expected error for unsafe table name")
	}
}

func TestImportCSVMultiWithTableOverrideErrors(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "imp.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	_, err := ImportCSV(path, []string{tinyCSV, tinyCSV}, "people")
	if err == nil {
		t.Fatal("expected error using --table override with multiple CSVs")
	}
}

func TestAppendCSV(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "app.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "people"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := AppendCSV(path, []string{tinyCSV}, "people")
	if err != nil {
		t.Fatalf("AppendCSV: %v", err)
	}
	if len(got) != 1 || got[0].Table != "people" || got[0].Rows != 3 {
		t.Errorf("got %+v, want one entry people/3", got)
	}
	entries, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if entries[0].Rows != 6 {
		t.Errorf("post-append row total = %d, want 6", entries[0].Rows)
	}
}

func TestAppendCSVMissingTable(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "app.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	_, err := AppendCSV(path, []string{tinyCSV}, "ghost")
	if err == nil {
		t.Fatal("expected error appending to missing table")
	}
	msg := err.Error()
	if !strings.Contains(msg, "does not exist") {
		t.Errorf("error %q should mention 'does not exist'", msg)
	}
	if !strings.Contains(msg, "sci db add") {
		t.Errorf("error %q should suggest `sci db add`", msg)
	}
	if !strings.Contains(msg, "ghost") {
		t.Errorf("error %q should name the table", msg)
	}
}

// seedDuckDBWithView builds a duckdb containing a base table and a view
// over it. Used by the drop-view / rename-view tests.
func seedDuckDBWithView(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "viewy.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	if _, err := ImportCSV(path, []string{tinyCSV}, "people"); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	sql := "ATTACH '" + path + "' AS d; CREATE VIEW d.people_view AS SELECT id, name FROM d.people; DETACH d;"
	if _, err := runJSON(sql); err != nil {
		t.Fatalf("seed view: %v", err)
	}
	return path
}

func TestDropTableHandlesView(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := seedDuckDBWithView(t, dir)
	if err := DropTable(path, "people_view"); err != nil {
		t.Fatalf("DropTable on view: %v", err)
	}
	metas, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "people" {
		t.Errorf("expected only `people` to remain, got %+v", metas)
	}
}

func TestDropTableMissingErrorMentionsFile(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "drop.duckdb")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}
	err := DropTable(path, "ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "does not exist") {
		t.Errorf("error %q should mention 'does not exist'", msg)
	}
	if !strings.Contains(msg, "ghost") {
		t.Errorf("error %q should name the table", msg)
	}
	if !strings.Contains(msg, "drop.duckdb") {
		t.Errorf("error %q should name the file", msg)
	}
}

func TestRenameTableHandlesView(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := seedDuckDBWithView(t, dir)
	if err := RenameTable(path, "people_view", "humans_view"); err != nil {
		t.Fatalf("RenameTable on view: %v", err)
	}
	metas, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	byName := map[string]bool{}
	for _, m := range metas {
		byName[m.Name] = true
	}
	if byName["people_view"] {
		t.Error("old view name still present")
	}
	if !byName["humans_view"] {
		t.Error("renamed view not found")
	}
}

func TestAppendCSVOntoViewErrors(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	dir := t.TempDir()
	path := seedDuckDBWithView(t, dir)
	_, err := AppendCSV(path, []string{tinyCSV}, "people_view")
	if err == nil {
		t.Fatal("expected error appending to a view")
	}
	if !strings.Contains(err.Error(), "view") {
		t.Errorf("error %q should mention the target is a view", err)
	}
}
