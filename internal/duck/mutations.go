package duck

// mutations.go — write primitives for `.duckdb` files: create an empty
// database, reset, drop/rename tables, and import or append CSVs. All
// shell out to the duckdb CLI via runJSON; the entire mutation runs in
// one duckdb invocation so a failure rolls back.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// ImportEntry reports how many rows landed in a given table during an
// ImportCSV or AppendCSV call.
type ImportEntry struct {
	Table string
	Rows  int
}

// CreateEmpty materialises an empty duckdb file at path. Refuses to
// overwrite an existing file — callers should use Reset for that.
func CreateEmpty(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	return touchEmpty(path)
}

// Reset deletes path if it exists and creates a fresh empty duckdb file.
// Idempotent: missing files are treated as already-clean.
func Reset(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	// duckdb writes a small WAL alongside the main file on some platforms.
	_ = os.Remove(path + ".wal")
	return touchEmpty(path)
}

// touchEmpty produces an empty duckdb file by ATTACHing and immediately
// DETACHing. duckdb writes the header on ATTACH, so the file persists.
func touchEmpty(path string) error {
	sql := fmt.Sprintf("ATTACH '%s' AS d; DETACH d;", sqlEscape(path))
	if _, err := runJSON(sql); err != nil {
		return fmt.Errorf("create duckdb at %s: %w", path, err)
	}
	return nil
}

// DropTable removes a table or view from a duckdb file. Uses
// DROP VIEW when the named object is a view. Returns a uniform
// "does not exist" error if missing.
func DropTable(path, table string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	metas, err := Info(path)
	if err != nil {
		return err
	}
	meta, ok := findTable(metas, table)
	if !ok {
		return fmt.Errorf("table %q does not exist in %s", table, filepath.Base(path))
	}
	keyword := "TABLE"
	if meta.IsView {
		keyword = "VIEW"
	}
	sql := fmt.Sprintf(`ATTACH '%s' AS d; DROP %s d."%s"; DETACH d;`,
		sqlEscape(path), keyword, table)
	if _, err := runJSON(sql); err != nil {
		return err
	}
	return nil
}

// RenameTable renames a table or view. Surfaces uniform "does not
// exist" / "already exists" errors instead of duckdb's CLI vocabulary.
// Uses ALTER VIEW when the source is a view.
func RenameTable(path, oldName, newName string) error {
	if !store.IsSafeIdentifier(oldName) {
		return fmt.Errorf("invalid table name %q", oldName)
	}
	if !store.IsSafeIdentifier(newName) {
		return fmt.Errorf("invalid table name %q", newName)
	}
	metas, err := Info(path)
	if err != nil {
		return err
	}
	meta, ok := findTable(metas, oldName)
	if !ok {
		return fmt.Errorf("table %q does not exist in %s", oldName, filepath.Base(path))
	}
	if _, taken := findTable(metas, newName); taken {
		return fmt.Errorf("table %q already exists in %s", newName, filepath.Base(path))
	}
	keyword := "TABLE"
	if meta.IsView {
		keyword = "VIEW"
	}
	sql := fmt.Sprintf(`ATTACH '%s' AS d; ALTER %s d."%s" RENAME TO "%s"; DETACH d;`,
		sqlEscape(path), keyword, oldName, newName)
	if _, err := runJSON(sql); err != nil {
		return err
	}
	return nil
}

// findTable returns the matching TableMeta entry or false. Linear
// scan; mutation paths only call this against the small (~tens of
// tables) catalog returned by Info.
func findTable(metas []TableMeta, name string) (TableMeta, bool) {
	return lo.Find(metas, func(m TableMeta) bool { return m.Name == name })
}

// ImportCSV creates one new table per csv via CREATE TABLE AS SELECT.
// Returns an error if any target table already exists in the database.
// When tableOverride is non-empty exactly one csv must be supplied; the
// resulting table takes that name. Otherwise table names are derived
// from each csv's basename via TableNameFromFile.
func ImportCSV(path string, csvPaths []string, tableOverride string) ([]ImportEntry, error) {
	if tableOverride != "" && len(csvPaths) > 1 {
		return nil, fmt.Errorf("--table can only be used with a single CSV file")
	}
	plans, err := planImports(csvPaths, tableOverride)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "ATTACH '%s' AS d;", sqlEscape(path))
	for _, p := range plans {
		// Bare CREATE TABLE (no IF NOT EXISTS) — duckdb returns
		// "Table with name X already exists" on collision, which is
		// the signal the caller wraps into a `sci db append` hint.
		fmt.Fprintf(&b, ` CREATE TABLE d."%s" AS SELECT * FROM read_csv_auto('%s');`,
			p.table, sqlEscape(p.csv))
	}
	b.WriteString(" DETACH d;")
	if _, err := runJSON(b.String()); err != nil {
		return nil, err
	}

	return tallyRows(path, plans)
}

// AppendCSV inserts the rows of each csv into an existing duckdb table.
// Errors with a uniform "does not exist" message if the target table is
// missing, or "cannot append to view" if the target is a view. The
// override / single-file constraint matches ImportCSV.
func AppendCSV(path string, csvPaths []string, tableOverride string) ([]ImportEntry, error) {
	if tableOverride != "" && len(csvPaths) > 1 {
		return nil, fmt.Errorf("--table can only be used with a single CSV file")
	}
	plans, err := planImports(csvPaths, tableOverride)
	if err != nil {
		return nil, err
	}

	metas, err := Info(path)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		meta, ok := findTable(metas, p.table)
		if !ok {
			return nil, fmt.Errorf("table %q does not exist in %s — use `sci db add` to create it", p.table, filepath.Base(path))
		}
		if meta.IsView {
			return nil, fmt.Errorf("cannot append to view %q in %s", p.table, filepath.Base(path))
		}
	}

	// Capture pre-append counts so the returned ImportEntry reports the
	// delta rather than the total.
	before, err := tallyRows(path, plans)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "ATTACH '%s' AS d;", sqlEscape(path))
	for _, p := range plans {
		fmt.Fprintf(&b, ` INSERT INTO d."%s" SELECT * FROM read_csv_auto('%s');`,
			p.table, sqlEscape(p.csv))
	}
	b.WriteString(" DETACH d;")
	if _, err := runJSON(b.String()); err != nil {
		return nil, err
	}

	after, err := tallyRows(path, plans)
	if err != nil {
		return nil, err
	}
	beforeByTable := lo.SliceToMap(before, func(e ImportEntry) (string, int) {
		return e.Table, e.Rows
	})
	return lo.Map(after, func(e ImportEntry, _ int) ImportEntry {
		return ImportEntry{Table: e.Table, Rows: e.Rows - beforeByTable[e.Table]}
	}), nil
}

// importPlan pairs a source csv with its destination table.
type importPlan struct {
	csv   string
	table string
}

// planImports validates inputs and resolves the table name for each csv.
func planImports(csvPaths []string, tableOverride string) ([]importPlan, error) {
	if len(csvPaths) == 0 {
		return nil, fmt.Errorf("no CSV files provided")
	}
	plans := make([]importPlan, 0, len(csvPaths))
	for _, csv := range csvPaths {
		name := tableOverride
		if name == "" {
			name = store.TableNameFromFile(csv)
		}
		if !store.IsSafeIdentifier(name) {
			return nil, fmt.Errorf("invalid table name %q", name)
		}
		plans = append(plans, importPlan{csv: csv, table: name})
	}
	return plans, nil
}

// tallyRows reports COUNT(*) for each planned destination table. Used
// before+after AppendCSV to compute deltas and after ImportCSV to
// report final row counts.
func tallyRows(path string, plans []importPlan) ([]ImportEntry, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	parts := lo.Map(plans, func(p importPlan, _ int) string {
		return fmt.Sprintf(`SELECT '%s' AS name, COUNT(*) AS n FROM d.%s`,
			sqlEscape(p.table), quoteIdent(p.table))
	})
	sql := fmt.Sprintf("ATTACH '%s' AS d (READ_ONLY); %s",
		sqlEscape(path), strings.Join(parts, " UNION ALL "))
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse row counts: %w", err)
	}
	byName := lo.SliceToMap(rows, func(r struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}) (string, int) {
		return r.Name, r.N
	})
	return lo.Map(plans, func(p importPlan, _ int) ImportEntry {
		return ImportEntry{Table: p.table, Rows: byName[p.table]}
	}), nil
}
