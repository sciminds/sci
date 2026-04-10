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
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

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

// tableNameFromFile derives a SQL-safe table name from a CSV filename.
func tableNameFromFile(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.ReplaceAll(name, "-", "_")
	var clean strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			clean.WriteRune(r)
		}
	}
	name = clean.String()
	if name == "" {
		name = "imported"
	}
	if r, _ := utf8.DecodeRuneInString(name); unicode.IsDigit(r) {
		name = "_" + name
	}
	return name
}
