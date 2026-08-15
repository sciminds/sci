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
	"github.com/sciminds/sci/internal/store"
	"github.com/sciminds/sci/internal/store/sqlite"
)

// withStore opens the database for writing, calls fn, and closes it. Use it
// only for verbs that mutate — [sqlite.Open] converts a rollback-journal
// database to WAL, which is a permanent change to the user's file.
func withStore(path string, fn func(store.DataStore) error) error {
	if err := requireDatabase(path); err != nil {
		return err
	}
	s, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return fn(s)
}

// withReadStore is [withStore] for verbs that only look: the database is
// opened through [sqlite.OpenReadOnly], so the file comes back byte-for-byte
// identical and any accidental write fails with [store.ErrReadOnly].
func withReadStore(path string, fn func(store.DataStore) error) error {
	if err := requireDatabase(path); err != nil {
		return err
	}
	s, err := sqlite.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return fn(s)
}
