// Package data provides SQLite-backed storage for the database manager.
//
// It implements the [github.com/sciminds/cli/internal/tui/dbtui/data.DataStore] interface with two
// concrete types:
//
//   - [SQLiteStore] for persistent .db files (created via [OpenStore]),
//     using pocketbase/dbx as the query builder
//   - [FileViewStore] for viewing flat files (CSV, TSV, JSON, JSONL) as
//     ephemeral in-memory SQLite tables
//
// Connection setup uses the pure-Go modernc.org/sqlite driver (no CGO)
// with WAL mode, foreign keys, and a 5-second busy timeout. Use [OpenFile]
// or [OpenMemory] for raw dbx.DB connections.
package data

import (
	"database/sql"
	"fmt"
	"sync/atomic"

	"github.com/pocketbase/dbx"

	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// memDBCounter generates unique names so each OpenMemory call gets an isolated
// shared-cache database that supports multiple concurrent connections.
var memDBCounter atomic.Uint64

// OpenMemory opens an in-memory SQLite database. Useful for tests.
func OpenMemory() (*dbx.DB, error) {
	// Use a unique shared-cache URI so multiple connections from the pool
	// see the same database (plain ":memory:" gives each conn its own DB).
	id := memDBCounter.Add(1)
	dsn := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", id)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open in-memory sqlite: %w", err)
	}
	if err := configureSQLite(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return dbx.NewFromDB(sqlDB, "sqlite"), nil
}

// OpenFile opens a file-backed SQLite database.
func OpenFile(path string) (*dbx.DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := configureSQLite(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return dbx.NewFromDB(sqlDB, "sqlite"), nil
}

// configureSQLite sets pragmas and connection limits for SQLite.
func configureSQLite(db *sql.DB) error {
	// WAL mode supports concurrent readers; allow up to 4 so introspection
	// queries (COUNT, PRAGMA table_info) can run in parallel.
	db.SetMaxOpenConns(4)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA mmap_size=268435456", // 256 MB — lets SQLite memory-map large files
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}
