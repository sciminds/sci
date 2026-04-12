package local

import (
	"strings"
	"testing"
)

// TestReadOnlyConnection verifies that the SQLite connection opened by
// Open rejects write operations at the database level. This is the
// runtime half of the read-only firewall; the Reader interface is the
// compile-time half.
func TestReadOnlyConnection(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	writes := []struct {
		label string
		sql   string
	}{
		{"INSERT", `INSERT INTO items (itemID, itemTypeID, libraryID, key) VALUES (999, 1, 1, 'HACK0001')`},
		{"UPDATE", `UPDATE items SET key='HACK0002' WHERE itemID=10`},
		{"DELETE", `DELETE FROM items WHERE itemID=10`},
		{"DROP", `DROP TABLE items`},
		{"CREATE", `CREATE TABLE hacked (id INTEGER)`},
	}

	for _, tc := range writes {
		t.Run(tc.label, func(t *testing.T) {
			_, err := db.db.Exec(tc.sql)
			if err == nil {
				t.Fatalf("%s succeeded — read-only connection did not reject the write", tc.label)
			}
			// SQLite should mention "readonly" or "query_only" in the error.
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "readonly") && !strings.Contains(msg, "read-only") && !strings.Contains(msg, "query_only") {
				t.Fatalf("%s failed with unexpected error (want read-only rejection): %v", tc.label, err)
			}
		})
	}
}

// TestReaderInterfaceSatisfied is a belt-and-suspenders check that *DB
// continues to satisfy Reader. The compile-time assertion in reader.go
// catches this too, but an explicit test makes the contract visible in
// test output and prevents silent breakage if the assertion line is
// accidentally removed.
func TestReaderInterfaceSatisfied(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Assign to the interface — if *DB drifts from Reader this won't compile.
	var r Reader = db
	if r.LibraryID() != db.LibraryID() {
		t.Fatal("interface dispatch returned wrong LibraryID")
	}
}
