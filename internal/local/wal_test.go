package local

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// buildWALFixture seeds a fresh zotero.sqlite in its own directory, switches it
// to WAL journalling, and applies a commit that is deliberately left
// UNCHECKPOINTED — the state a running Zotero desktop leaves between
// checkpoints, and the one that made sci report already-resolved duplicates as
// still open (EJO-933).
//
// The writer connection stays open for the life of the test on purpose:
// SQLite checkpoints when the last connection closes, which would fold the
// commit into the main file and erase the condition under test.
func buildWALFixture(t *testing.T, apply string) string {
	t.Helper()

	dir := t.TempDir()
	if err := seedFixture(dir); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	path := filepath.Join(dir, "zotero.sqlite")

	w, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	// One pooled connection, held open, so the WAL is never checkpointed by
	// an idle-connection reap mid-test.
	w.SetMaxIdleConns(1)
	w.SetMaxOpenConns(1)

	var mode string
	if err := w.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	if _, err := w.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatalf("disable autocheckpoint: %v", err)
	}
	if _, err := w.Exec(apply); err != nil {
		t.Fatalf("apply %q: %v", apply, err)
	}

	// Guard the premise: if the -wal is empty the commit already landed in
	// the main file and the test would pass for the wrong reason.
	st, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatalf("stat -wal (the fixture must leave one behind): %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("-wal is empty — the commit was checkpointed, test proves nothing")
	}
	return dir
}

// TestImmutableOpenCannotSeeUncheckpointedWAL pins the known, deliberate
// limitation that PendingWAL exists to report (EJO-933): under
// mode=ro&immutable=1 a deletion sitting in the WAL is invisible, so a
// trashed item still reads as live.
//
// This asserts the *current* behaviour on purpose. Immutable mode is chosen
// for availability — a plain mode=ro connection cannot open at all while
// Zotero holds its exclusive lock — and the staleness is surfaced as a
// warning instead of being closed. If someone later drops immutable=1, this
// test fails and should be read as the prompt to re-litigate that trade
// (availability, mid-run lock acquisition, the -shm written into the user's
// Zotero directory), not as a test to quietly flip.
func TestImmutableOpenCannotSeeUncheckpointedWAL(t *testing.T) {
	dir := buildWALFixture(t, `INSERT INTO deletedItems VALUES (10)`)

	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.Read("AAAA1111")
	if _, missing := errors.AsType[*ItemNotFoundError](err); missing {
		t.Fatal("Read saw the WAL deletion — immutable mode is no longer skipping the WAL; " +
			"see this test's doc comment before updating it")
	}
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned no item and no error")
	}

	// The warning signal is what makes the staleness survivable.
	if _, ok := db.PendingWAL(); !ok {
		t.Error("the read was stale and PendingWAL reported nothing — the gap would be silent")
	}
}

// TestPendingWALReportsUncheckpointedBytes covers the warning signal: callers
// need to know a read may be behind the live library even when the fallback
// path had to use immutable mode.
func TestPendingWALReportsUncheckpointedBytes(t *testing.T) {
	dir := buildWALFixture(t, `INSERT INTO deletedItems VALUES (10)`)

	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	n, ok := db.PendingWAL()
	if !ok {
		t.Fatal("PendingWAL reported no signal, want the uncheckpointed -wal size")
	}
	if n <= 0 {
		t.Fatalf("PendingWAL = %d bytes, want > 0", n)
	}
}

// TestPendingWALAbsentWithoutWAL pins the honest-negative: the shared fixture
// is journal_mode=delete with no sibling -wal, so there is no staleness claim
// to make. A missing signal must read as "no claim", never as a warning.
func TestPendingWALAbsentWithoutWAL(t *testing.T) {
	dir := buildFixture(t)

	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if n, ok := db.PendingWAL(); ok {
		t.Fatalf("PendingWAL reported %d bytes on a DB with no -wal, want no signal", n)
	}
}

// hashFile returns a content digest, or "" when the file is absent.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestOpenNeverMutatesTheDatabase is the safety pin for dropping immutable=1.
// A WAL-aware connection needs the -shm shared-memory index, which is the one
// new file sci may touch in the user's Zotero directory — but neither
// zotero.sqlite nor its -wal may change, ever. This is a real user library;
// a read that mutates it is unacceptable regardless of freshness benefits.
func TestOpenNeverMutatesTheDatabase(t *testing.T) {
	dir := buildWALFixture(t, `INSERT INTO deletedItems VALUES (10)`)
	path := filepath.Join(dir, "zotero.sqlite")

	beforeDB := hashFile(t, path)
	beforeWAL := hashFile(t, path+"-wal")

	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Exercise real read paths, not just the connection handshake.
	if _, err := db.Read("BBBB2222"); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := db.Stats(); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := hashFile(t, path); got != beforeDB {
		t.Error("zotero.sqlite was modified by a read-only Open")
	}
	if got := hashFile(t, path+"-wal"); got != beforeWAL {
		t.Error("the -wal was modified by a read-only Open — uncheckpointed Zotero writes are at risk")
	}
}

// TestWALConnectionRejectsWrites extends the read-only guarantee to a
// WAL-mode database. TestReadOnlyConnection covers the delete-mode fixture;
// this pins the same contract against a file Zotero is actively journalling,
// which is the shape that matters for a real library.
func TestWALConnectionRejectsWrites(t *testing.T) {
	dir := buildWALFixture(t, `INSERT INTO deletedItems VALUES (10)`)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, tc := range []struct{ label, sql string }{
		{"INSERT", `INSERT INTO items (itemID, itemTypeID, libraryID, key) VALUES (999, 1, 1, 'HACK0001')`},
		{"UPDATE", `UPDATE items SET key='HACK0002' WHERE itemID=10`},
		{"DELETE", `DELETE FROM items WHERE itemID=10`},
		{"DROP", `DROP TABLE items`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if _, err := db.db.Exec(tc.sql); err == nil {
				t.Fatalf("%s succeeded — the WAL-aware connection is not read-only", tc.label)
			}
		})
	}
}
