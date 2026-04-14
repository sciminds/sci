package view

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/zot/local"
)

// findRepoRoot walks up from cwd looking for go.mod. Same pattern as the
// hygiene real-library tests — kept local to avoid cross-package test deps.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from cwd")
		}
		dir = parent
	}
}

// TestStore_RealLibrary exercises the view.Store against the user's real
// zotero.sqlite at the repo root. Gated behind SLOW=1 and skipped cleanly
// when the file is absent, matching the convention in zot/hygiene.
func TestStore_RealLibrary(t *testing.T) {
	t.Parallel()
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library view smoke")
	}
	root := findRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "zotero.sqlite")); err != nil {
		t.Skipf("no ./zotero.sqlite at repo root — skipping real-db test")
	}

	db, err := local.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	store := New(db, time.UTC)
	defer func() { _ = store.Close() }()

	// Row count should match QueryTable.
	n, err := store.TableRowCount(TableName)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("real library reports zero rows — filter likely broken")
	}

	cols, rows, _, ids, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != n {
		t.Errorf("QueryTable returned %d rows, TableRowCount said %d", len(rows), n)
	}
	if len(cols) != 8 {
		t.Errorf("expected 8 columns, got %d", len(cols))
	}
	if len(ids) != len(rows) {
		t.Errorf("rowIDs len %d != rows len %d", len(ids), len(rows))
	}

	// Row 0's Date Added must match the human format (we pin UTC so the
	// test is deterministic regardless of runner timezone).
	dateFmt := regexp.MustCompile(`^\d{2}/\d{2}/\d{2}, \d{1,2}:\d{2}(am|pm)$`)
	if !dateFmt.MatchString(rows[0][4]) {
		t.Errorf("row 0 Date Added %q does not match human format", rows[0][4])
	}

	// Descending date added sort: row 0 should be >= row 1 lexicographically
	// on the underlying dateAdded. Proxy via the formatted strings is
	// unreliable (month-day-year ordering), so re-read raw via ListViewRows.
	raw, err := db.ListViewRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 {
		t.Skip("not enough rows to verify sort")
	}
	if raw[0].DateAdded < raw[1].DateAdded {
		t.Errorf("sort order broken: %q < %q", raw[0].DateAdded, raw[1].DateAdded)
	}

	t.Logf("real library: %d rows, first row title=%q", n, rows[0][3])
}
