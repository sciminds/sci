package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"

	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// hashFile returns the SHA-256 of a file's bytes. Tests use it to prove a
// read verb left the user's database untouched — an error-free return is
// not proof, because the WAL pragma succeeded while rewriting the file.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newRollbackJournalDB writes a small SQLite database that stays in the
// default (delete) journal mode — the shape almost every database not
// created by sci has on disk.
func newRollbackJournalDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range []string{
		"PRAGMA journal_mode = DELETE",
		"CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO t (name) VALUES ('a'), ('b')",
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

// TestInfoLeavesTheDatabaseByteIdentical is the data-safety pin: `sci db
// info` is purely informational, so the file it inspects must come back
// byte-for-byte identical (no journal-mode rewrite, no WAL sidecars).
func TestInfoLeavesTheDatabaseByteIdentical(t *testing.T) {
	path := newRollbackJournalDB(t)
	before := hashFile(t, path)

	result, err := Info(path)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(result.Tables) != 1 || result.Tables[0].Name != "t" || result.Tables[0].Rows != 2 {
		t.Fatalf("Info returned %+v, want one table t with 2 rows", result.Tables)
	}

	if after := hashFile(t, path); after != before {
		t.Errorf("Info rewrote the database: before %s, after %s", before, after)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			t.Errorf("Info created %s — a read verb must not touch the database's sidecars", filepath.Base(sidecar))
		}
	}
}

// TestInfoReadsAWALDatabase — databases sci itself created are in WAL mode,
// so the read-only path has to open those too.
func TestInfoReadsAWALDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := conn.Exec("CREATE TABLE w (id INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	before := hashFile(t, path)
	result, err := Info(path)
	if err != nil {
		t.Fatalf("Info on a WAL database: %v", err)
	}
	if len(result.Tables) != 1 || result.Tables[0].Name != "w" {
		t.Fatalf("Info returned %+v, want the table w", result.Tables)
	}
	if after := hashFile(t, path); after != before {
		t.Errorf("Info rewrote the WAL database: before %s, after %s", before, after)
	}
}

// codedFrom extracts the CodedError an error path is expected to carry.
func codedFrom(t *testing.T, err error) *cmdutil.CodedError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("error %q carries no cmdutil.CodedError (envelope would say code=runtime)", err)
	}
	return coded
}

func TestInfoMissingFileIsNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	coded := codedFrom(t, mustFail(t, missing))
	if coded.Code != cmdutil.CodeNotFound {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeNotFound)
	}
	if coded.Try == "" && coded.Fix == "" {
		t.Error("a missing database should tell the caller what to do next")
	}
}

func TestInfoNonDatabaseIsUsageWithoutSQLiteInternals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notadatabase.txt")
	if err := os.WriteFile(path, []byte("hello, not a database\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	coded := codedFrom(t, mustFail(t, path))
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	for _, leak := range []string{"PRAGMA", "journal_mode", "(26)"} {
		if strings.Contains(coded.Message, leak) {
			t.Errorf("message %q leaks SQLite internals (%q)", coded.Message, leak)
		}
	}
}

// TestInfoOnADirectoryIsUsage — pointing a database verb at a directory is a
// command-line mistake, not a runtime failure.
func TestInfoOnADirectoryIsUsage(t *testing.T) {
	coded := codedFrom(t, mustFail(t, t.TempDir()))
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
}

// mustFail runs Info and requires it to fail.
func mustFail(t *testing.T, path string) error {
	t.Helper()
	result, err := Info(path)
	if err == nil {
		t.Fatalf("Info(%q) unexpectedly succeeded: %+v", path, result)
	}
	return err
}
