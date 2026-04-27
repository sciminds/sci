// Package db implements a general-purpose SQLite database manager.
//
// Users import CSV/TSV files as tables and browse data in an interactive TUI.
//
// Key functions:
//
//   - [AddCSV] imports CSV files as new tables
//   - [RunTUI] launches the interactive data browser
package db

import (
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/db/data"
)

// withStore opens the database, calls fn, and closes it.
func withStore(path string, fn func(data.DataStore) error) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	store, err := data.OpenStore(path)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return fn(store)
}

// validateTableName checks that a user-supplied table name is safe for SQL.
func validateTableName(name string) error {
	if !data.IsSafeIdentifier(name) {
		return fmt.Errorf("invalid table name: %q (only alphanumerics, underscores, and spaces allowed)", name)
	}
	return nil
}
