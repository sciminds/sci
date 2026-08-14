// Package local provides read-only access to a local zotero.sqlite file.
//
// The database is opened with mode=ro&immutable=1 so we neither touch the
// WAL nor contend with the running Zotero desktop app's locks.
//
// That choice has a price, and it is not optional to know about: immutable=1
// tells SQLite the file cannot change, which makes it skip WAL processing
// entirely. Every change Zotero has committed but not yet checkpointed is
// therefore INVISIBLE to this package. Zotero checkpoints on a clean quit,
// so the gap is usually empty — but while the app is running it can be
// arbitrarily large, and reads will confidently return superseded data.
//
// Dropping immutable=1 is not the fix. Zotero holds an exclusive lock, so a
// plain mode=ro connection fails to open at all while the app is running
// ("database is locked"), and a connection opened while Zotero is closed can
// start failing mid-run if the user launches it. Availability of the read
// matters more than its freshness; a stale answer plus an honest warning
// beats an intermittent one.
//
// So the gap is reported rather than closed: [DB.PendingWAL] sizes what this
// handle cannot see, and callers turn it into a staleness warning (see
// walStaleWarning in internal/zot/cli). Anything that silently trusts a local
// read is asserting the WAL is empty — check it instead.
//
// Every query is scoped to a single libraryID, chosen at Open time via a
// LibrarySelector:
//
//   - ForPersonal()               — the user's personal library (libraries.type='user')
//   - ForGroupByAPIID(apiGroupID) — a group by its Zotero Web API groupID
//     (joins the groups table to resolve)
//   - ForAll(apiGroupID)          — the merged personal+group read pool
//
// Open requires an explicit selector — there is no implicit default. Callers
// that want the personal library pass ForPersonal().
//
// This package uses raw database/sql (not pocketbase/dbx) — read-only
// SQLite doesn't need dbx ergonomics.
package local

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	db *sql.DB
	// libraryID is the primary (first-resolved) library. Binding it directly
	// is correct for every selector but ForAll, so a query that does must
	// call requireSingleLibrary first — see that method. Queries that span
	// the pool go through libIn instead.
	libraryID int64
	// libraryIDs is every library the handle spans; len>1 only for ForAll.
	libraryIDs []int64
	// userLibraryID backs scopeLabel — the one libraries row with
	// type='user', resolved at init regardless of selector.
	userLibraryID int64
	schemaVer     int

	// path is the zotero.sqlite this handle was opened from; PendingWAL
	// stats its sibling -wal.
	path string

	// Fulltext table detection (lazy, cached).
	ftsOnce sync.Once
	hasFTS  bool
}

// Open opens zotero.sqlite inside dataDir in immutable mode and resolves
// the target libraryID via sel. Returns an error if the file does not
// exist or the selector cannot resolve a libraryID.
func Open(dataDir string, sel LibrarySelector) (*DB, error) {
	if sel.resolve == nil {
		return nil, fmt.Errorf("invalid LibrarySelector (zero value); use ForPersonal, ForGroupByAPIID, or ForAll")
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

	d := &DB{db: sqldb, path: path}
	if err := d.init(sel); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return d, nil
}

// PendingWAL returns the size in bytes of the sibling -wal file and whether
// there is a signal at all. Because [Open] always uses immutable mode, a
// non-empty WAL is exactly the set of committed Zotero changes this handle
// cannot see — callers surface it as a staleness warning.
//
// ok=false means no claim can be made: the database is not in WAL mode, or
// the log is fully checkpointed, or the stat failed. A freshness signal must
// never fail (or slow) a read, so errors collapse into "no claim" rather
// than propagating.
func (d *DB) PendingWAL() (int64, bool) {
	st, err := os.Stat(d.path + "-wal")
	if err != nil || st.Size() == 0 {
		return 0, false
	}
	return st.Size(), true
}

func (d *DB) init(sel LibrarySelector) error {
	// 1. Schema version sanity check.
	var ver int
	err := d.db.QueryRow("SELECT version FROM version WHERE schema='userdata'").Scan(&ver)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	d.schemaVer = ver

	// 2. Resolve the target library (or libraries) via the selector.
	libIDs, err := sel.resolve(d.db)
	if err != nil {
		return err
	}
	if len(libIDs) == 0 {
		return fmt.Errorf("selector %s resolved no libraries", sel.label)
	}
	d.libraryIDs = libIDs
	d.libraryID = libIDs[0]

	// 3. Pin the user library's ID so every row can be stamped with its
	// scope label ("personal" vs "shared") regardless of selector mode.
	// Every Zotero database has exactly one user library.
	if err := d.db.QueryRow("SELECT libraryID FROM libraries WHERE type='user' LIMIT 1").
		Scan(&d.userLibraryID); err != nil {
		return fmt.Errorf("resolve user library ID: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (d *DB) Close() error { return d.db.Close() }

// LibraryID returns the pinned library ID selected at Open time, or 0
// for a multi-library handle (ForAll) — no single library owns a merged
// pool, so result shells emitting a top-level library_id read 0 and
// per-row [Item.Library] is the provenance that matters.
func (d *DB) LibraryID() int64 {
	if len(d.libraryIDs) > 1 {
		return 0
	}
	return d.libraryID
}

// requireSingleLibrary rejects op on a merged (ForAll) handle.
//
// A query that binds [DB.libraryID] answers for one library. Run against a
// handle spanning several, it does not fail — it answers personal-only, which
// is indistinguishable from a correct short answer and so is the worst
// available outcome. Callers that bind libraryID call this first; callers that
// went through [DB.libIn] do not need it, because their WHERE clause already
// widens to every library in the handle.
//
// op names the operation for the error ("list tags", "read note"). The fix is
// never to catch this error — it is either to open a single-library handle, or
// to convert the query through libIn so the merged answer is real.
//
// TestSingleLibraryQueriesAreGuarded enforces the pairing, so a new query
// cannot reintroduce the silent case by forgetting this call.
func (d *DB) requireSingleLibrary(op string) error {
	if len(d.libraryIDs) > 1 {
		return fmt.Errorf("%s: this query resolves one library at a time, but the handle spans %d "+
			"(open with ForPersonal/ForGroupByAPIID, or convert the query through libIn)",
			op, len(d.libraryIDs))
	}
	return nil
}

// libIn returns a SQL fragment filtering alias.libraryID to the handle's
// resolved libraries, plus its bind args. The single-library form stays
// `= ?` so query plans (and EXPLAIN output) are unchanged for the common
// case.
func (d *DB) libIn(alias string) (string, []any) {
	if len(d.libraryIDs) == 1 {
		return alias + ".libraryID = ?", []any{d.libraryID}
	}
	ph, args := inClause(d.libraryIDs)
	return alias + ".libraryID IN (" + ph + ")", args
}

// scopeLabel names the library a row belongs to: "personal" for the user
// library, "shared" for any group. The vocabulary matches the --library
// flag so agents can round-trip a row's provenance into a scoped call.
func (d *DB) scopeLabel(libraryID int64) string {
	if libraryID == d.userLibraryID {
		return "personal"
	}
	return "shared"
}

// SchemaVersion returns the userdata schema version from the version table.
func (d *DB) SchemaVersion() int { return d.schemaVer }

// SchemaOutOfRange reports whether the on-disk schema is outside the range
// this package has been tested against. Callers should warn but not abort.
func (d *DB) SchemaOutOfRange() bool {
	return d.schemaVer < MinTestedSchemaVersion || d.schemaVer > MaxTestedSchemaVersion
}

// LastSync returns when Zotero last synced this database (the version
// table's 'lastsync' row, unix seconds). ok is false when the row is absent
// (never-synced library, pre-sync Zotero, synthetic fixture) — callers must
// fail open: no timestamp means no staleness claim, never a warning.
func (d *DB) LastSync() (time.Time, bool) {
	var unix int64
	err := d.db.QueryRow("SELECT version FROM version WHERE schema='lastsync'").Scan(&unix)
	if err != nil || unix <= 0 {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}
