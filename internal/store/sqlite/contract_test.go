package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/store/contracttest"
)

// setupContract builds the shared contract fixture in a fresh SQLite file
// and returns it as a store.DataStore. Cleanup is registered via
// t.Cleanup so each subtest gets an isolated database.
func setupContract(t *testing.T) store.DataStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ddl := []string{
		`CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT, score REAL)`,
		`INSERT INTO people VALUES (1, 'alice', 3.14), (2, 'bob', 2.72), (3, 'carol', NULL)`,
		`CREATE TABLE extras (k TEXT, v INTEGER)`,
		`INSERT INTO extras VALUES ('a', 1), ('b', 2)`,
	}
	for _, stmt := range ddl {
		if _, err := s.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	return s
}

func TestStoreContract(t *testing.T) {
	contracttest.Run(t, setupContract)
}
