package db

// commands.go — local database operations: info, tables, add, create, reset,
// delete, rename, and the interactive TUI viewer.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/db/data"
	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
)

// Info returns database metadata including table listing.
func Info(dbPath string) (*InfoResult, error) {
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

// Tables returns table summaries for a database.
func Tables(dbPath string) (*TablesResult, error) {
	var result TablesResult
	err := withStore(dbPath, func(store data.DataStore) error {
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

// DeleteTable drops a table from the database.
func DeleteTable(table, dbPath string) (*MutationResult, error) {
	if err := validateTableName(table); err != nil {
		return nil, err
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
	err := withStore(dbPath, func(store data.DataStore) error {
		return store.RenameTable(oldName, newName)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{OK: true, Message: fmt.Sprintf("renamed %q → %q", oldName, newName)}, nil
}

// AddCSV imports CSV files into a SQLite database.
func AddCSV(csvFiles []string, dbPath string, tableName string) (*MutationResult, error) {
	if tableName != "" && len(csvFiles) > 1 {
		return nil, fmt.Errorf("--table can only be used with a single CSV file")
	}

	var imported []string
	err := withStore(dbPath, func(store data.DataStore) error {
		for _, csvPath := range csvFiles {
			name := tableName
			if name == "" {
				name = tableNameFromFile(csvPath)
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

// Create creates an empty SQLite database at the given path, ensuring the
// parent directory exists. Returns an error if the file already exists.
func Create(dbPath string) (*MutationResult, error) {
	if _, err := os.Stat(dbPath); err == nil {
		return &MutationResult{OK: false, Message: fmt.Sprintf("%s already exists — use 'sci db reset' to clear it", dbPath)}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	// Open and immediately close to create an empty database file.
	store, err := data.OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	_ = store.Close()
	return &MutationResult{OK: true, Message: fmt.Sprintf("created %s", dbPath)}, nil
}

// Reset deletes a SQLite database and recreates it empty.
func Reset(dbPath string) (*MutationResult, error) {
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Remove(dbPath); err != nil {
			return nil, fmt.Errorf("remove database: %w", err)
		}
		// SQLite WAL mode may create auxiliary files alongside the main file.
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
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
// SQLite databases are opened directly. If initialTab is non-empty the viewer
// opens on that tab instead of the first one.
func RunTUI(dbPath string, initialTab string) error {
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}

	var store data.DataStore
	if data.IsViewableFile(dbPath) {
		s, err := data.OpenFileStore(dbPath)
		if err != nil {
			return err
		}
		store = s
	} else {
		s, err := data.OpenStore(dbPath)
		if err != nil {
			return err
		}
		store = s
	}
	defer func() { _ = store.Close() }()

	return dbtui.Run(store, dbPath, dbtui.WithInitialTab(initialTab))
}
