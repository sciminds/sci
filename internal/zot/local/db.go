// Package local provides read-only access to a local zotero.sqlite file.
//
// The database is opened with mode=ro&immutable=1 so we neither touch the
// WAL nor contend with the running Zotero desktop app's locks. Every query
// is scoped to a single libraryID, chosen at Open time via a LibrarySelector:
//
//   - ForPersonal()             — the user's personal library (libraries.type='user')
//   - ForGroup(sqliteLibraryID) — a specific group by its SQLite libraryID
//   - ForGroupByAPIID(apiGroupID) — a group by its Zotero Web API groupID
//     (joins the groups table to resolve)
//
// Open requires an explicit selector — there is no implicit default. Callers
// that want the personal library pass ForPersonal().
//
// This package uses raw database/sql (not pocketbase/dbx) — a documented
// exception alongside internal/tui/dbtui/data and internal/markdb.
package local

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Known userdata schema versions we've tested against. Zotero bumps these
// between releases; if the on-disk DB is outside this range we log a warning
// via the caller but do not fail.
const (
	MinTestedSchemaVersion = 120
	MaxTestedSchemaVersion = 130
)

// DB is a read-only handle to a zotero.sqlite file, pinned to a libraryID.
type DB struct {
	db        *sql.DB
	libraryID int64
	schemaVer int

	// Fulltext table detection (lazy, cached).
	ftsOnce sync.Once
	hasFTS  bool
}

// Open opens zotero.sqlite inside dataDir in immutable mode and resolves
// the target libraryID via sel. Returns an error if the file does not
// exist or the selector cannot resolve a libraryID.
func Open(dataDir string, sel LibrarySelector) (*DB, error) {
	if sel.resolve == nil {
		return nil, fmt.Errorf("invalid LibrarySelector (zero value); use ForPersonal or ForGroup")
	}

	path := filepath.Join(dataDir, "zotero.sqlite")
	// mode=ro forbids writes; immutable=1 tells SQLite the file will not
	// change during the connection's lifetime, which skips WAL processing
	// entirely and avoids any lock contention with Zotero desktop.
	// _pragma=query_only(1) is belt-and-suspenders in case something sneaks
	// a write past mode=ro.
	dsn := "file:" + path + "?mode=ro&immutable=1&_pragma=query_only(1)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}

	d := &DB{db: sqldb}
	if err := d.init(sel); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) init(sel LibrarySelector) error {
	// 1. Schema version sanity check.
	var ver int
	err := d.db.QueryRow("SELECT version FROM version WHERE schema='userdata'").Scan(&ver)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	d.schemaVer = ver

	// 2. Resolve the target library via the selector.
	libID, err := sel.resolve(d.db)
	if err != nil {
		return err
	}
	d.libraryID = libID
	return nil
}

// Close releases the database handle.
func (d *DB) Close() error { return d.db.Close() }

// LibraryID returns the pinned library ID selected at Open time.
func (d *DB) LibraryID() int64 { return d.libraryID }

// SchemaVersion returns the userdata schema version from the version table.
func (d *DB) SchemaVersion() int { return d.schemaVer }

// SchemaOutOfRange reports whether the on-disk schema is outside the range
// this package has been tested against. Callers should warn but not abort.
func (d *DB) SchemaOutOfRange() bool {
	return d.schemaVer < MinTestedSchemaVersion || d.schemaVer > MaxTestedSchemaVersion
}
