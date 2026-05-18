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
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/db/data"
	"github.com/sciminds/cli/internal/duck"
	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
)

// isDuckDB reports whether path's extension marks it as a duckdb file.
func isDuckDB(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".duckdb")
}

// ---------------------------------------------------------------------------
// Info
// ---------------------------------------------------------------------------

// Info returns database metadata including table listing. SQLite files
// open through the local SQLite store; .duckdb files route through the
// duckdb CLI since modernc.org/sqlite cannot read them.
func Info(dbPath string) (*InfoResult, error) {
	if isDuckDB(dbPath) {
		return infoDuckDB(dbPath)
	}
	return infoSQLite(dbPath)
}

func infoSQLite(dbPath string) (*InfoResult, error) {
	var result InfoResult

	err := withStore(dbPath, func(store data.DataStore) error {
		fi, err := os.Stat(dbPath)
		if err != nil {
			return err
		}
		result.DBPath = dbPath
		result.SizeMB = float64(fi.Size()) / (1024 * 1024)

		summaries, err := store.TableSummaries()
		if err != nil {
			return err
		}
		type viewLister interface{ IsView(string) bool }
		type virtualLister interface{ IsVirtual(string) bool }
		vl, hasViews := store.(viewLister)
		vtl, hasVirtuals := store.(virtualLister)
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

// DeleteTable drops a table from the database.
func DeleteTable(table, dbPath string) (*MutationResult, error) {
	if err := validateTableName(table); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		if err := duck.DropTable(dbPath, table); err != nil {
			return nil, err
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("dropped %q", table)}, nil
	}
	err := withStore(dbPath, func(store data.DataStore) error {
		return store.DropTable(table)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: fmt.Sprintf("dropped %q", table)}, nil
}

// RenameTable renames a table in the database.
func RenameTable(oldName, newName, dbPath string) (*MutationResult, error) {
	if err := validateTableName(oldName); err != nil {
		return nil, err
	}
	if err := validateTableName(newName); err != nil {
		return nil, err
	}
	if isDuckDB(dbPath) {
		if err := duck.RenameTable(dbPath, oldName, newName); err != nil {
			return nil, err
		}
		return &MutationResult{OK: true, Message: fmt.Sprintf("renamed %q → %q", oldName, newName)}, nil
	}
	err := withStore(dbPath, func(store data.DataStore) error {
		return store.RenameTable(oldName, newName)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: fmt.Sprintf("renamed %q → %q", oldName, newName)}, nil
}

// ---------------------------------------------------------------------------
// AddCSV / AppendCSV
// ---------------------------------------------------------------------------

// AddCSV imports one or more CSV files as new tables. Errors if any
// target table already exists — callers can use AppendCSV to add rows
// to an existing table.
func AddCSV(csvFiles []string, dbPath string, tableName string) (*MutationResult, error) {
	if tableName != "" && len(csvFiles) > 1 {
		return nil, fmt.Errorf("--table can only be used with a single CSV file")
	}
	if isDuckDB(dbPath) {
		return addCSVDuckDB(csvFiles, dbPath, tableName)
	}
	return addCSVSQLite(csvFiles, dbPath, tableName)
}

// AppendCSV inserts one or more CSV files into existing tables. Errors
// if a target table does not exist.
func AppendCSV(csvFiles []string, dbPath string, tableName string) (*MutationResult, error) {
	if tableName != "" && len(csvFiles) > 1 {
		return nil, fmt.Errorf("--table can only be used with a single CSV file")
	}
	if isDuckDB(dbPath) {
		return appendCSVDuckDB(csvFiles, dbPath, tableName)
	}
	return appendCSVSQLite(csvFiles, dbPath, tableName)
}

func addCSVSQLite(csvFiles []string, dbPath, tableName string) (*MutationResult, error) {
	var imported []string
	err := withStore(dbPath, func(store data.DataStore) error {
		// Resolve names + pre-check collisions so we emit a friendly
		// "use sci db append" error instead of SQLite's raw collision message.
		names, err := store.TableNames()
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
			if err := store.ImportCSV(absCSV, name); err != nil {
				return fmt.Errorf("import %q: %w", csvPath, err)
			}
			count, err := store.TableRowCount(name)
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
	err := withStore(dbPath, func(store data.DataStore) error {
		for _, csvPath := range csvFiles {
			name := nameForCSV(csvPath, tableName)
			absCSV, err := filepath.Abs(csvPath)
			if err != nil {
				return fmt.Errorf("resolve path %q: %w", csvPath, err)
			}
			if _, err := os.Stat(absCSV); err != nil {
				return err
			}
			before, err := store.TableRowCount(name)
			if err != nil {
				return err
			}
			if err := store.AppendCSV(absCSV, name); err != nil {
				return fmt.Errorf("append %q: %w", csvPath, err)
			}
			after, err := store.TableRowCount(name)
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
	for _, csv := range csvFiles {
		if _, err := os.Stat(csv); err != nil {
			return nil, err
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
	return data.TableNameFromFile(csvPath)
}

// collisionErr produces the standard "table already exists, use append"
// error used by both SQLite and duckdb add paths.
func collisionErr(name, dbPath string) error {
	return fmt.Errorf(
		"table %q already exists in %s — use `sci db append` to add rows, or `sci db delete %s %s` to drop it first",
		name, dbPath, name, dbPath,
	)
}

// ---------------------------------------------------------------------------
// Create / Reset
// ---------------------------------------------------------------------------

// Create creates an empty database at the given path. SQLite for any
// non-.duckdb extension; duckdb otherwise. Surfaces (rather than
// silently consumes) the case where a stray duckdb .wal file exists
// without the matching main file — that pattern points at a crashed
// session and the user should clean it up explicitly.
func Create(dbPath string) (*MutationResult, error) {
	if _, err := os.Stat(dbPath); err == nil {
		return &MutationResult{OK: false, Message: fmt.Sprintf("%s already exists — use 'sci db reset' to clear it", dbPath)}, nil
	}
	if isDuckDB(dbPath) {
		if _, err := os.Stat(dbPath + ".wal"); err == nil {
			return nil, fmt.Errorf(
				"%s.wal exists but %s does not — looks like a crashed duckdb session. Remove the .wal file or run `sci db reset %s` to start fresh",
				dbPath, dbPath, dbPath,
			)
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
	store, err := data.OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	_ = store.Close()
	return &MutationResult{OK: true, Message: fmt.Sprintf("created %s", dbPath)}, nil
}

// Reset deletes the database file (if present) and recreates an empty one.
func Reset(dbPath string) (*MutationResult, error) {
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
	store, err := data.OpenStore(dbPath)
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
// Flat files (CSV, JSON, etc.) are opened read-only via a file-aware store;
// SQLite databases are opened directly. .duckdb files are mirrored into
// a tempfile SQLite database and opened read-only — duckdb's richer
// types (STRUCT/LIST/INTERVAL) flatten to TEXT in the mirror. If
// initialTab is non-empty the viewer opens on that tab instead of the
// first one.
func RunTUI(dbPath string, initialTab string) error {
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}

	if isDuckDB(dbPath) {
		return runTUIDuckDB(dbPath, initialTab)
	}

	var store data.DataStore
	switch {
	case data.IsViewableFile(dbPath):
		s, err := data.OpenFileStore(dbPath)
		if err != nil {
			return err
		}
		store = s
	default:
		s, err := data.OpenStore(dbPath)
		if err != nil {
			return err
		}
		store = s
	}
	defer func() { _ = store.Close() }()

	return dbtui.Run(store, dbPath, dbtui.WithInitialTab(initialTab))
}

// runTUIDuckDB mirrors a .duckdb file into a tempfile SQLite database
// and opens that mirror through dbtui with read-only forced on. The
// title bar still shows the original .duckdb path so the user sees
// what they actually opened. Tempfile is removed on exit.
//
// Any columns whose duckdb types collapsed to TEXT in the mirror
// (STRUCT, LIST, MAP, INTERVAL, UNION) are summarised to stderr after
// the TUI exits — placed *after* so the note is the last thing the
// user sees when they leave the viewer.
func runTUIDuckDB(dbPath, initialTab string) error {
	dir, err := os.MkdirTemp("", "sci-duckdb-mirror-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	mirror := filepath.Join(dir, "mirror.db")
	if err := duck.BuildSQLiteMirror(dbPath, mirror); err != nil {
		return err
	}

	// Compute the lossy-column note up-front so the duckdb file is
	// inspected exactly once even if dbtui returns an error.
	lossy, lossyErr := duck.LossyColumns(dbPath)

	store, err := data.OpenStore(mirror)
	if err != nil {
		return fmt.Errorf("open duckdb mirror: %w", err)
	}
	defer func() { _ = store.Close() }()

	runErr := dbtui.Run(store, dbPath,
		dbtui.WithInitialTab(initialTab),
		dbtui.WithReadOnly(),
	)

	if lossyErr == nil && len(lossy) > 0 {
		fmt.Fprintln(os.Stderr, formatLossyNote(lossy))
	}
	return runErr
}

// formatLossyNote builds the post-exit warning summarising columns
// that flattened to TEXT in the SQLite mirror. Inline list when the
// set is small; otherwise a per-table count.
func formatLossyNote(cols []duck.LossyColumn) string {
	const inlineLimit = 6
	if len(cols) <= inlineLimit {
		parts := lo.Map(cols, func(c duck.LossyColumn, _ int) string {
			return fmt.Sprintf("%s.%s (%s)", c.Table, c.Column, c.Type)
		})
		return fmt.Sprintf(
			"note: %d column(s) flattened to TEXT in the read-only mirror: %s\n"+
				"      full types via `sci db cols <file> --table <name>`",
			len(cols), strings.Join(parts, ", "),
		)
	}
	byTable := lo.GroupBy(cols, func(c duck.LossyColumn) string { return c.Table })
	tableParts := lo.MapToSlice(byTable, func(table string, cs []duck.LossyColumn) string {
		return fmt.Sprintf("%s: %d", table, len(cs))
	})
	return fmt.Sprintf(
		"note: %d column(s) flattened to TEXT in the read-only mirror (%s)\n"+
			"      full types via `sci db cols <file> --table <name>`",
		len(cols), strings.Join(tableParts, ", "),
	)
}
