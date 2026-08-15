package db

// commands.go — local database operations: info, add, append, create,
// reset, delete, rename, and the interactive TUI viewer. Each verb
// dispatches on file extension: .duckdb routes through internal/duck;
// everything else falls through to the SQLite store.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/duck"
	"github.com/sciminds/sci/internal/store"
	duckstore "github.com/sciminds/sci/internal/store/duck"
	"github.com/sciminds/sci/internal/store/sqlite"
	dbtui "github.com/sciminds/sci/internal/tui/dbtui/app"
)

// isDuckDB reports whether path's extension marks it as a duckdb file.
func isDuckDB(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".duckdb")
}

// isParquet reports whether path is a Parquet file. Parquet is columnar
// binary, so it routes through the duckdb store (which reads it natively)
// rather than the pure-Go SQLite file view.
func isParquet(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".parquet")
}

// ---------------------------------------------------------------------------
// Info
// ---------------------------------------------------------------------------

// Info returns database metadata including table listing. SQLite files
// open through the local SQLite store; .duckdb files route through the
// duckdb CLI since modernc.org/sqlite cannot read them.
//
// Info is purely informational and leaves the file byte-for-byte identical:
// the SQLite path opens read-only (see [withReadStore]) rather than through
// the writer's WAL-converting open.
func Info(dbPath string) (*InfoResult, error) {
	if isDuckDB(dbPath) {
		return infoDuckDB(dbPath)
	}
	return infoSQLite(dbPath)
}

func infoSQLite(dbPath string) (*InfoResult, error) {
	var result InfoResult

	err := withReadStore(dbPath, func(s store.DataStore) error {
		fi, err := os.Stat(dbPath)
		if err != nil {
			return err
		}
		result.DBPath = dbPath
		result.SizeMB = float64(fi.Size()) / (1024 * 1024)

		summaries, err := s.TableSummaries()
		if err != nil {
			return err
		}
		type viewLister interface{ IsView(string) bool }
		type virtualLister interface{ IsVirtual(string) bool }
		vl, hasViews := s.(viewLister)
		vtl, hasVirtuals := s.(virtualLister)
		result.Tables = make([]TableEntry, len(summaries))
		for i, s := range summaries {
			result.Tables[i] = TableEntry{Name: s.Name, Rows: s.Rows, Columns: s.Columns}
			if hasViews {
				result.Tables[i].IsView = vl.IsView(s.Name)
			}
			if hasVirtuals {
				result.Tables[i].IsVirtual = vtl.IsVirtual(s.Name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func infoDuckDB(dbPath string) (*InfoResult, error) {
	if err := requireDatabase(dbPath); err != nil {
		return nil, err
	}
	fi, err := os.Stat(dbPath)
	if err != nil {
		return nil, err
	}
	metas, err := duck.Info(dbPath)
	if err != nil {
		return nil, err
	}
	return &InfoResult{
		DBPath: dbPath,
		SizeMB: float64(fi.Size()) / (1024 * 1024),
		Tables: lo.Map(metas, func(m duck.TableMeta, _ int) TableEntry {
			return TableEntry{Name: m.Name, Rows: m.Rows, Columns: m.Columns, IsView: m.IsView}
		}),
	}, nil
}

// ---------------------------------------------------------------------------
// DeleteTable / RenameTable
// ---------------------------------------------------------------------------

// DeleteTable drops a table from the database. A table the database does
// not have is a not-found error naming the database, not the driver's
// "SQL logic error: no such table".
func DeleteTable(table, dbPath string) (*MutationResult, error) {
	if err := validateTableName(table); err != nil {
		return nil, err
	}
	if err := requireDatabase(dbPath); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		if err := requireTable(dbPath, table); err != nil {
			return nil, err
		}
		if err := duck.DropTable(dbPath, table); err != nil {
			return nil, err
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("dropped %q", table)}, nil
	}
	err := withStore(dbPath, func(s store.DataStore) error {
		names, err := s.TableNames()
		if err != nil {
			return err
		}
		if !slices.Contains(names, table) {
			return errTableNotFound(table, dbPath)
		}
		return s.DropTable(table)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: fmt.Sprintf("dropped %q", table)}, nil
}

// RenameTable renames a table in the database. Both halves are checked
// first: a missing source is not-found, an occupied destination is a usage
// error the caller fixes by choosing another name.
func RenameTable(oldName, newName, dbPath string) (*MutationResult, error) {
	if err := validateTableName(oldName); err != nil {
		return nil, err
	}
	if err := validateTableName(newName); err != nil {
		return nil, err
	}
	if err := requireDatabase(dbPath); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		names, err := duckTableNames(dbPath)
		if err != nil {
			return nil, err
		}
		if err := checkRenameTargets(names, oldName, newName, dbPath); err != nil {
			return nil, err
		}
		if err := duck.RenameTable(dbPath, oldName, newName); err != nil {
			return nil, err
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("renamed %q → %q", oldName, newName)}, nil
	}
	err := withStore(dbPath, func(s store.DataStore) error {
		names, err := s.TableNames()
		if err != nil {
			return err
		}
		if err := checkRenameTargets(names, oldName, newName, dbPath); err != nil {
			return err
		}
		return s.RenameTable(oldName, newName)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: fmt.Sprintf("renamed %q → %q", oldName, newName)}, nil
}

// checkRenameTargets validates a rename against the database's table list.
func checkRenameTargets(names []string, oldName, newName, dbPath string) error {
	if !slices.Contains(names, oldName) {
		return errTableNotFound(oldName, dbPath)
	}
	if slices.Contains(names, newName) {
		return errTableExists(newName, dbPath)
	}
	return nil
}

// duckTableNames lists the tables and views in a .duckdb file.
func duckTableNames(dbPath string) ([]string, error) {
	metas, err := duck.Info(dbPath)
	if err != nil {
		return nil, err
	}
	return lo.Map(metas, func(m duck.TableMeta, _ int) string { return m.Name }), nil
}

// requireTable reports a not-found error when a .duckdb file has no such table.
func requireTable(dbPath, table string) error {
	names, err := duckTableNames(dbPath)
	if err != nil {
		return err
	}
	if !slices.Contains(names, table) {
		return errTableNotFound(table, dbPath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AddCSV / AppendCSV
// ---------------------------------------------------------------------------

// AddCSV imports one or more CSV files as new tables. Errors if any
// target table already exists — callers can use AppendCSV to add rows
// to an existing table.
func AddCSV(csvFiles []string, dbPath string, tableName string) (*MutationResult, error) {
	if err := checkImportInputs(csvFiles, dbPath, tableName); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		return addCSVDuckDB(csvFiles, dbPath, tableName)
	}
	return addCSVSQLite(csvFiles, dbPath, tableName)
}

// AppendCSV inserts one or more CSV files into existing tables. Errors
// if a target table does not exist.
func AppendCSV(csvFiles []string, dbPath string, tableName string) (*MutationResult, error) {
	if err := checkImportInputs(csvFiles, dbPath, tableName); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		return appendCSVDuckDB(csvFiles, dbPath, tableName)
	}
	return appendCSVSQLite(csvFiles, dbPath, tableName)
}

// checkImportInputs validates everything the caller named before either
// import path opens anything: the --table override's arity, the target
// database, and each source file.
func checkImportInputs(csvFiles []string, dbPath, tableName string) error {
	if tableName != "" && len(csvFiles) > 1 {
		return cmdutil.Coded(cmdutil.CodeUsage, "--table can only be used with a single CSV file").
			WithTry("drop --table (each file becomes a table named after it), or import one file at a time")
	}
	if err := requireDatabase(dbPath); err != nil {
		return err
	}
	for _, csvPath := range csvFiles {
		if err := requireInputFile(csvPath); err != nil {
			return err
		}
	}
	return nil
}

func addCSVSQLite(csvFiles []string, dbPath, tableName string) (*MutationResult, error) {
	var imported []string
	err := withStore(dbPath, func(s store.DataStore) error {
		// Resolve names + pre-check collisions so we emit a friendly
		// "use sci db append" error instead of SQLite's raw collision message.
		names, err := s.TableNames()
		if err != nil {
			return err
		}
		existing := lo.SliceToMap(names, func(n string) (string, bool) { return n, true })

		for _, csvPath := range csvFiles {
			name := nameForCSV(csvPath, tableName)
			if existing[name] {
				return collisionErr(name, dbPath)
			}
			absCSV, err := filepath.Abs(csvPath)
			if err != nil {
				return fmt.Errorf("resolve path %q: %w", csvPath, err)
			}
			if _, err := os.Stat(absCSV); err != nil {
				return err
			}
			if err := s.ImportCSV(absCSV, name); err != nil {
				return fmt.Errorf("import %q: %w", csvPath, err)
			}
			count, err := s.TableRowCount(name)
			if err != nil {
				return err
			}
			imported = append(imported, fmt.Sprintf("%s (%d rows)", name, count))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: "imported: " + strings.Join(imported, ", ")}, nil
}

func addCSVDuckDB(csvFiles []string, dbPath, tableName string) (*MutationResult, error) {
	// Pre-check: refuse if any target table already exists.
	metas, err := duck.Info(dbPath)
	if err != nil {
		return nil, err
	}
	existing := lo.SliceToMap(metas, func(m duck.TableMeta) (string, bool) {
		return m.Name, true
	})
	for _, csv := range csvFiles {
		name := nameForCSV(csv, tableName)
		if existing[name] {
			return nil, collisionErr(name, dbPath)
		}
	}
	for _, csv := range csvFiles {
		if _, err := os.Stat(csv); err != nil {
			return nil, err
		}
	}

	entries, err := duck.ImportCSV(dbPath, csvFiles, tableName)
	if err != nil {
		return nil, err
	}
	parts := lo.Map(entries, func(e duck.ImportEntry, _ int) string {
		return fmt.Sprintf("%s (%d rows)", e.Table, e.Rows)
	})
	return &MutationResult{OK: true, Message: "imported: " + strings.Join(parts, ", ")}, nil
}

func appendCSVSQLite(csvFiles []string, dbPath, tableName string) (*MutationResult, error) {
	var appended []string
	err := withStore(dbPath, func(s store.DataStore) error {
		names, err := s.TableNames()
		if err != nil {
			return err
		}
		for _, csvPath := range csvFiles {
			name := nameForCSV(csvPath, tableName)
			if !slices.Contains(names, name) {
				return errAppendTableMissing(name, csvPath, dbPath)
			}
			absCSV, err := filepath.Abs(csvPath)
			if err != nil {
				return fmt.Errorf("resolve path %q: %w", csvPath, err)
			}
			before, err := s.TableRowCount(name)
			if err != nil {
				return err
			}
			if err := s.AppendCSV(absCSV, name); err != nil {
				return fmt.Errorf("append %q: %w", csvPath, err)
			}
			after, err := s.TableRowCount(name)
			if err != nil {
				return err
			}
			appended = append(appended, fmt.Sprintf("%s (+%d rows)", name, after-before))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: "appended: " + strings.Join(appended, ", ")}, nil
}

func appendCSVDuckDB(csvFiles []string, dbPath, tableName string) (*MutationResult, error) {
	names, err := duckTableNames(dbPath)
	if err != nil {
		return nil, err
	}
	for _, csvPath := range csvFiles {
		name := nameForCSV(csvPath, tableName)
		if !slices.Contains(names, name) {
			return nil, errAppendTableMissing(name, csvPath, dbPath)
		}
	}
	entries, err := duck.AppendCSV(dbPath, csvFiles, tableName)
	if err != nil {
		return nil, err
	}
	parts := lo.Map(entries, func(e duck.ImportEntry, _ int) string {
		return fmt.Sprintf("%s (+%d rows)", e.Table, e.Rows)
	})
	return &MutationResult{OK: true, Message: "appended: " + strings.Join(parts, ", ")}, nil
}

// nameForCSV returns the override when non-empty, otherwise derives a
// safe table name from the CSV's basename.
func nameForCSV(csvPath, override string) string {
	if override != "" {
		return override
	}
	return store.TableNameFromFile(csvPath)
}

// collisionErr produces the standard "table already exists, use append"
// error used by both SQLite and duckdb add paths. It codes usage: the
// caller picks a different verb or a different table name and retries.
// The two ways out mean neither is a Fix — appending rows and dropping the
// table are different intents, and sci refuses to guess which was meant.
func collisionErr(name, dbPath string) error {
	return cmdutil.Coded(cmdutil.CodeUsage, "table %q already exists in %s", name, dbPath).
		WithTry(fmt.Sprintf(
			"use `sci db append` to add rows to it, `--table <name>` to import under another name, or `sci db delete %s %s` to drop it first",
			name, dbPath))
}

// errAppendTableMissing is the mirror of [collisionErr]: append needs a
// table that already exists, and names the add verb that would create it.
func errAppendTableMissing(name, csvPath, dbPath string) error {
	return cmdutil.Coded(cmdutil.CodeNotFound, "no table %q in %s to append to", name, dbPath).
		WithFix(fmt.Sprintf("sci db add --table %s %s %s", name, csvPath, dbPath))
}

// ---------------------------------------------------------------------------
// Create / Reset
// ---------------------------------------------------------------------------

// Create creates an empty database at the given path. SQLite for any
// non-.duckdb extension; duckdb otherwise. Surfaces (rather than
// silently consumes) the case where a stray duckdb .wal file exists
// without the matching main file — that pattern points at a crashed
// session and the user should clean it up explicitly.
//
// An existing path is a refusal, not a result: creating over it would be
// destructive, so it codes usage (exit 2 — rewrite the command line) rather
// than returning a "successful" envelope whose payload says it failed.
func Create(dbPath string) (*MutationResult, error) {
	if _, err := os.Stat(dbPath); err == nil {
		return nil, cmdutil.Coded(cmdutil.CodeUsage, "%s already exists", dbPath).
			WithTry(fmt.Sprintf("pick another path, or clear this one with `sci db reset %s`", dbPath))
	}
	if isDuckDB(dbPath) {
		if _, err := os.Stat(dbPath + ".wal"); err == nil {
			return nil, cmdutil.Coded(cmdutil.CodeUsage,
				"%s.wal exists but %s does not — a crashed duckdb session left it behind", dbPath, dbPath).
				WithTry(fmt.Sprintf("remove the .wal file, or run `sci db reset %s` to start fresh", dbPath))
		}
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	if isDuckDB(dbPath) {
		if err := duck.CreateEmpty(dbPath); err != nil {
			return nil, fmt.Errorf("create database: %w", err)
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("created %s", dbPath)}, nil
	}
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	_ = store.Close()
	return &MutationResult{OK: true, Message: fmt.Sprintf("created %s", dbPath)}, nil
}

// Reset deletes the database file (if present) and recreates an empty one.
//
// It refuses to delete a file that is not a database: `sci db reset` on a
// mistyped path (a thesis, a CSV) would otherwise destroy it, and the
// confirmation prompt reads as if the target were a database either way.
func Reset(dbPath string) (*MutationResult, error) {
	if _, err := os.Stat(dbPath); err == nil {
		if err := requireDatabase(dbPath); err != nil {
			return nil, err
		}
	}
	if isDuckDB(dbPath) {
		if err := duck.Reset(dbPath); err != nil {
			return nil, err
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("reset %s", dbPath)}, nil
	}
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Remove(dbPath); err != nil {
			return nil, fmt.Errorf("remove database: %w", err)
		}
		// SQLite WAL mode may create auxiliary files alongside the main file.
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	_ = store.Close()
	return &MutationResult{OK: true, Message: fmt.Sprintf("reset %s", dbPath)}, nil
}

// ---------------------------------------------------------------------------
// TUI viewer
// ---------------------------------------------------------------------------

// RunTUI launches the interactive database viewer.
// Flat files (CSV, JSON, etc.) are opened via the pure-Go SQLite file
// view; Parquet routes through a native duckdb file view (columnar binary
// the SQLite path can't parse) and opens read-only. SQLite databases are
// opened directly. .duckdb files open through a native subprocess-backed
// [duckstore.Store]. If initialTab is non-empty the viewer opens on that
// tab instead of the first one.
func RunTUI(dbPath string, initialTab string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cmdutil.Coded(cmdutil.CodeNotFound, "file not found: %s", dbPath).
				WithTry("check the path — `sci view` opens .db/.sqlite/.duckdb databases and csv/tsv/json/jsonl/parquet files")
		}
		return err
	}

	var ds store.DataStore
	switch {
	case isDuckDB(dbPath):
		if err := requireDatabase(dbPath); err != nil {
			return err
		}
		s, err := duckstore.Open(dbPath)
		if err != nil {
			return err
		}
		ds = s
	case isParquet(dbPath):
		s, err := duckstore.OpenFileView(dbPath)
		if err != nil {
			return err
		}
		ds = s
	case sqlite.IsViewableFile(dbPath):
		s, err := sqlite.OpenFileView(dbPath)
		if err != nil {
			return err
		}
		ds = s
	default:
		if err := requireDatabase(dbPath); err != nil {
			return err
		}
		s, err := sqlite.Open(dbPath)
		if err != nil {
			return err
		}
		ds = s
	}
	defer func() { _ = ds.Close() }()

	opts := []dbtui.RunOption{dbtui.WithInitialTab(initialTab)}
	return dbtui.Run(ds, dbPath, opts...)
}
