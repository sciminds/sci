package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/sci/internal/store"
)

// hashFile returns the SHA-256 of a file's bytes.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rollbackJournalCopy copies the shared fixture and forces it back into the
// default (delete) journal mode, so a WAL conversion is visible in the hash.
func rollbackJournalCopy(t *testing.T) string {
	t.Helper()
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Exec("PRAGMA journal_mode = DELETE"); err != nil {
		t.Fatalf("set delete journal: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		_ = os.Remove(sidecar)
	}
	return path
}

// TestOpenReadOnlyLeavesTheFileUntouched — the read-only open must not
// rewrite the journal mode (or create sidecars), which is what makes it
// safe for informational verbs like `sci db info`.
func TestOpenReadOnlyLeavesTheFileUntouched(t *testing.T) {
	path := rollbackJournalCopy(t)
	before := hashFile(t, path)

	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("TableNames returned nothing — the fixture has tables")
	}
	if _, err := s.TableSummaries(); err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if after := hashFile(t, path); after != before {
		t.Errorf("OpenReadOnly rewrote the database: before %s, after %s", before, after)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			t.Errorf("OpenReadOnly created %s", filepath.Base(sidecar))
		}
	}
}

// TestOpenReadOnlyRefusesWrites — every mutation must fail, and fail with
// [store.ErrReadOnly] so callers can branch on it.
func TestOpenReadOnlyRefusesWrites(t *testing.T) {
	path := rollbackJournalCopy(t)
	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = s.Close() }()

	mutations := map[string]func() error{
		"Exec":             func() error { _, err := s.Exec("CREATE TABLE nope (a INTEGER)"); return err },
		"DropTable":        func() error { return s.DropTable("researchers") },
		"RenameTable":      func() error { return s.RenameTable("researchers", "people") },
		"CreateEmptyTable": func() error { return s.CreateEmptyTable("nope") },
		"UpdateCell":       func() error { return s.UpdateCell("researchers", "name", 1, nil, nil) },
		"DeleteRows":       func() error { _, err := s.DeleteRows("researchers", []store.RowIdentifier{{RowID: 1}}); return err },
		"InsertRows":       func() error { return s.InsertRows("researchers", []string{"name"}, [][]string{{"x"}}) },
		"RenameColumn":     func() error { return s.RenameColumn("researchers", "name", "moniker") },
		"DropColumn":       func() error { return s.DropColumn("researchers", "name") },
		"DeduplicateTable": func() error { _, err := s.DeduplicateTable("researchers"); return err },
		"CreateTableAs":    func() error { return s.CreateTableAs("copy", "SELECT 1") },
		"CreateViewAs":     func() error { return s.CreateViewAs("v", "SELECT 1") },
		"ImportCSV":        func() error { return s.ImportCSV("any.csv", "nope") },
		"AppendCSV":        func() error { return s.AppendCSV("any.csv", "researchers") },
		"ImportFile":       func() error { return s.ImportFile("any.csv", "nope") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			err := mutate()
			if err == nil {
				t.Fatalf("%s succeeded on a read-only store", name)
			}
			if !errors.Is(err, store.ErrReadOnly) {
				t.Errorf("%s returned %v, want store.ErrReadOnly", name, err)
			}
		})
	}

	if after := hashFile(t, path); after != hashFile(t, path) {
		t.Error("unstable hash")
	}
}

// TestOpenReadOnlyReadsAWALDatabase — databases sci created are in WAL
// mode, and the read-only path must open those too (a plain mode=ro
// connection cannot build the -shm sidecar it would need).
func TestOpenReadOnlyReadsAWALDatabase(t *testing.T) {
	path := copyFixture(t, "test.db") // Open leaves the copy in WAL mode
	w, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	before := hashFile(t, path)

	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly on a WAL database: %v", err)
	}
	if _, err := s.TableNames(); err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if after := hashFile(t, path); after != before {
		t.Errorf("OpenReadOnly rewrote the WAL database: before %s, after %s", before, after)
	}
}

// TestOpenReadOnlySeesCommittedWALChanges — a database with a live WAL
// still has to answer with current data, not the pre-checkpoint snapshot.
func TestOpenReadOnlySeesCommittedWALChanges(t *testing.T) {
	path := copyFixture(t, "test.db")
	w, err := Open(path) // keep this handle open so the WAL is not checkpointed
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = w.Close() }()
	if err := w.CreateEmptyTable("fresh_table"); err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}

	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = s.Close() }()
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	found := false
	for _, n := range names {
		if n == "fresh_table" {
			found = true
		}
	}
	if !found {
		t.Errorf("read-only handle missed a committed WAL write; saw %v", names)
	}
}
