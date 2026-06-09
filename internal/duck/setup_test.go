package duck

// setup_test.go — shared helpers and TestMain for the internal/duck test
// suite. Generates testdata/tiny.parquet at test start when the duckdb
// binary is available; verbs that need it skip cleanly otherwise via
// requireDuck.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	tinyCSV     = "testdata/tiny.csv"
	nanCSV      = "testdata/nan.csv" // float column with NaN/±Infinity (duckdb reads these as DOUBLE)
	tinyJSON    = "testdata/tiny.json"
	tinyXLSX    = "testdata/tiny.xlsx"
	tinyParquet = "testdata/tiny.parquet"
	tinyDB      = "testdata/tiny.db"         // generated; multi-table SQLite
	singleDB    = "testdata/single_table.db" // generated; single-table SQLite
	viewyDB     = "testdata/viewy.db"        // generated; SQLite w/ view that uses single-letter alias (regression)
	mixedDB     = "testdata/mixed_types.db"  // generated; SQLite w/ INTEGER column holding "" (promotes cleanly with NULLIF)
	dirtyDB     = "testdata/dirty_types.db"  // generated; SQLite w/ INTEGER column holding "abc" (must fall back to VARCHAR to preserve cell)
	tinyDuck    = "testdata/tiny.duckdb"     // generated; multi-table DuckDB
	singleDuck  = "testdata/single.duckdb"   // generated; single-table DuckDB
)

// requireDuck skips the calling test if the duckdb binary is not on PATH.
func requireDuck(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("duckdb binary not on PATH; install via `sci doctor` to run this test")
	}
}

// TestMain generates binary fixtures (parquet/sqlite/duckdb) from
// tiny.csv when duckdb is available, so dispatch tests have something
// real to point at without committing 800 KB+ blobs. If duckdb is
// missing, dependent tests skip via requireDuck.
func TestMain(m *testing.M) {
	if Available() {
		if err := generateBinaryFixtures(); err != nil {
			_, _ = os.Stderr.WriteString("WARN: failed to generate binary fixtures: " + err.Error() + "\n")
		}
	}
	os.Exit(m.Run())
}

func generateBinaryFixtures() error {
	csvAbs, _ := filepath.Abs(tinyCSV)
	for _, p := range []string{tinyParquet, tinyDB, singleDB, viewyDB, mixedDB, dirtyDB, tinyDuck, singleDuck} {
		_ = os.Remove(p)
	}

	pqAbs, _ := filepath.Abs(tinyParquet)
	if _, err := runJSON("COPY (SELECT * FROM read_csv_auto('" + csvAbs + "')) TO '" + pqAbs + "' (FORMAT PARQUET)"); err != nil {
		return err
	}

	// Multi-table SQLite: people + extras.
	multiSqliteAbs, _ := filepath.Abs(tinyDB)
	multiSqliteSQL := "ATTACH '" + multiSqliteAbs + "' AS s (TYPE SQLITE);" +
		"CREATE TABLE s.people AS SELECT * FROM read_csv_auto('" + csvAbs + "');" +
		"CREATE TABLE s.extras AS SELECT 'a' AS k, 1 AS v UNION ALL SELECT 'b', 2;" +
		"DETACH s"
	if _, err := runJSON(multiSqliteSQL); err != nil {
		return err
	}

	// Single-table SQLite.
	singleSqliteAbs, _ := filepath.Abs(singleDB)
	singleSqliteSQL := "ATTACH '" + singleSqliteAbs + "' AS s (TYPE SQLITE);" +
		"CREATE TABLE s.only_one AS SELECT * FROM read_csv_auto('" + csvAbs + "');" +
		"DETACH s"
	if _, err := runJSON(singleSqliteSQL); err != nil {
		return err
	}

	// SQLite with a view whose body uses a single-letter table alias.
	// This is a regression fixture for the bug where duckdb's sqlite
	// scanner translates view definitions during ATTACH and `SHOW
	// TABLES FROM s` fails with "syntax error at or near 's'". Build it
	// via the sqlite3 CLI when available; the table-listing tests skip
	// otherwise.
	if _, err := exec.LookPath("sqlite3"); err == nil {
		viewyAbs, _ := filepath.Abs(viewyDB)
		ddl := "CREATE TABLE similar (src TEXT, tgt TEXT, score REAL);" +
			"INSERT INTO similar VALUES ('a','b',0.9),('a','c',0.5);" +
			"CREATE TABLE other (k TEXT, v INT);" +
			"INSERT INTO other VALUES ('x',1);" +
			"CREATE VIEW similar_pairs AS " +
			"  SELECT 'paper_' || lower(s.src) AS src, " +
			"         'paper_' || lower(s.tgt) AS tgt, s.score FROM similar s;"
		cmd := exec.Command("sqlite3", viewyAbs)
		cmd.Stdin = strings.NewReader(ddl)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create viewy.db: %v\n%s", err, out)
		}

		// Mixed-type fixture: INTEGER column with one "" cell. NULLIF
		// normalises "" → NULL during the probe, so all non-empty cells
		// cast cleanly and the column promotes to BIGINT.
		mixedAbs, _ := filepath.Abs(mixedDB)
		mixedDDL := "CREATE TABLE demo (id INTEGER, name TEXT, age INTEGER);" +
			"INSERT INTO demo VALUES (1, 'alice', 30);" +
			"INSERT INTO demo VALUES (2, 'bob', '');" + // SQLite allows this
			"INSERT INTO demo VALUES (3, 'carol', 42);"
		mixedCmd := exec.Command("sqlite3", mixedAbs)
		mixedCmd.Stdin = strings.NewReader(mixedDDL)
		if out, err := mixedCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create mixed_types.db: %v\n%s", err, out)
		}

		// Dirty-type fixture: INTEGER column with a genuinely non-castable
		// value ("abc"). Promotion must fall back to VARCHAR for that
		// column so the original cell is preserved — that's the no-
		// data-loss promise.
		dirtyAbs, _ := filepath.Abs(dirtyDB)
		dirtyDDL := "CREATE TABLE demo (id INTEGER, name TEXT, age INTEGER);" +
			"INSERT INTO demo VALUES (1, 'alice', 30);" +
			"INSERT INTO demo VALUES (2, 'bob', 'abc');" + // non-castable
			"INSERT INTO demo VALUES (3, 'carol', 42);"
		dirtyCmd := exec.Command("sqlite3", dirtyAbs)
		dirtyCmd.Stdin = strings.NewReader(dirtyDDL)
		if out, err := dirtyCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create dirty_types.db: %v\n%s", err, out)
		}
	}

	// Multi-table DuckDB.
	multiDuckAbs, _ := filepath.Abs(tinyDuck)
	multiDuckSQL := "ATTACH '" + multiDuckAbs + "' AS d;" +
		"CREATE TABLE d.people AS SELECT * FROM read_csv_auto('" + csvAbs + "');" +
		"CREATE TABLE d.extras AS SELECT 'a' AS k, 1 AS v UNION ALL SELECT 'b', 2;" +
		"DETACH d"
	if _, err := runJSON(multiDuckSQL); err != nil {
		return err
	}

	// Single-table DuckDB.
	singleDuckAbs, _ := filepath.Abs(singleDuck)
	singleDuckSQL := "ATTACH '" + singleDuckAbs + "' AS d;" +
		"CREATE TABLE d.only_one AS SELECT * FROM read_csv_auto('" + csvAbs + "');" +
		"DETACH d"
	if _, err := runJSON(singleDuckSQL); err != nil {
		return err
	}
	return nil
}
