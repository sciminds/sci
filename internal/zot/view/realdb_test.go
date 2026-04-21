package view

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/tui/dbtui/match"
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

	db, err := local.Open(root, local.ForPersonal())
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

// TestStore_RealLibrary_MultiTokenSearch exercises the row-search match layer
// against the real Zotero library. Regression for the Zotero-parity fix: the
// query "Gossip drives" must find item 9093 ("Gossip drives vicarious learning
// and facilitates social connection"). Under the old sahilm-fuzzy semantics
// this worked by accident; under the new substring-AND-across-row semantics
// it works by design.
func TestStore_RealLibrary_MultiTokenSearch(t *testing.T) {
	t.Parallel()
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library multi-token search test")
	}
	root := findRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "zotero.sqlite")); err != nil {
		t.Skipf("no ./zotero.sqlite at repo root — skipping real-db test")
	}
	db, err := local.Open(root, local.ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	store := New(db, time.UTC)
	defer func() { _ = store.Close() }()

	_, rows, _, ids, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}

	const targetID int64 = 9093
	tokens := strings.Fields("Gossip drives")
	hitCount, targetMatched := 0, false
	for i, row := range rows {
		if _, ok := match.MatchRow(tokens, row, -1); ok {
			hitCount++
			if ids[i] == targetID {
				targetMatched = true
			}
		}
	}
	if !targetMatched {
		t.Errorf("item %d ('Gossip drives…') not matched by query %q", targetID, "Gossip drives")
	}
	if hitCount == 0 {
		t.Errorf("query %q matched zero rows — expected at least 1", "Gossip drives")
	}
	t.Logf("'Gossip drives' matched %d rows (including target=%v)", hitCount, targetMatched)
}

// TestStore_RealLibrary_FTSMultiTokenExact asserts that FTS on a multi-token
// query uses exact-word match, cutting the false-positive blast radius of
// prefix expansion (e.g. "drives" → "drove/driver/driven" across hundreds of
// unrelated PDFs). Regression for the FTS over-match that injected many
// unrelated rows after the debounced query finished.
func TestStore_RealLibrary_FTSMultiTokenExact(t *testing.T) {
	t.Parallel()
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library FTS test")
	}
	root := findRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "zotero.sqlite")); err != nil {
		t.Skipf("no ./zotero.sqlite at repo root — skipping real-db test")
	}
	db, err := local.Open(root, local.ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	store := New(db, time.UTC)
	defer func() { _ = store.Close() }()

	words := []string{"gossip", "drives"}
	prefixHits, err := store.SearchFulltext(TableName, words, false)
	if err != nil {
		t.Fatal(err)
	}
	exactHits, err := store.SearchFulltext(TableName, words, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FTS 'gossip drives': prefix=%d, exact=%d", len(prefixHits), len(exactHits))
	if len(exactHits) >= len(prefixHits) {
		t.Errorf("exact match should be strictly tighter than prefix: exact=%d, prefix=%d",
			len(exactHits), len(prefixHits))
	}
}
