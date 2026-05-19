package duck_test

// store_test.go — integration tests against a real duckdb subprocess.
// Each test generates its own fixture .duckdb via the `duckdb` CLI so we
// don't have to commit binary blobs. Tests skip cleanly when duckdb is
// not on PATH.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/store/duck"
)

// requireDuck skips the test if the duckdb CLI is missing.
func requireDuck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb binary not on PATH; install via `sci doctor` to run this test")
	}
}

// makeFixture writes a `.duckdb` file with a couple of tables and a
// view into a fresh temp dir, returning the path. `people(id PK, name,
// score)` has a primary key for row-level mutations; `extras(k, v)` is
// deliberately PK-less to exercise the read-only-without-PK code path.
// `recent_scores` is a view over people.
func makeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.duckdb")
	script := `CREATE TABLE people (id BIGINT PRIMARY KEY, name VARCHAR, score DOUBLE);
INSERT INTO people VALUES (1, 'alice', 3.14), (2, 'bob', 2.72), (3, 'carol', NULL);
CREATE TABLE extras (k VARCHAR, v INTEGER);
INSERT INTO extras VALUES ('a', 1), ('b', 2);
CREATE VIEW recent_scores AS SELECT name, score FROM people WHERE score IS NOT NULL;
`
	cmd := exec.Command("duckdb", path)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v\n%s", err, out)
	}
	return path
}

// ---------- tests ----------

func TestOpenAndClose(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("Close twice: %v", err)
	}
}

func TestOpenMissingBinary(t *testing.T) {
	// We can't easily un-install duckdb; instead, point the path at a
	// non-existent file and check the resulting error doesn't crash.
	requireDuck(t)
	_, err := duck.Open(filepath.Join(t.TempDir(), "does-not-exist.duckdb"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestTableNamesAndViews(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	slices.Sort(names)
	want := []string{"extras", "people", "recent_scores"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("TableNames = %v, want %v", names, want)
	}
	if !s.IsView("recent_scores") {
		t.Error("IsView(recent_scores) = false; want true")
	}
	if s.IsView("people") {
		t.Error("IsView(people) = true; want false")
	}
}

func TestTableColumns(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, err := s.TableColumns("people")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(cols) = %d, want 3", len(cols))
	}
	if cols[0].Name != "id" || cols[0].Type != "BIGINT" {
		t.Errorf("cols[0] = %+v; want id/BIGINT", cols[0])
	}
	if cols[1].Name != "name" || cols[1].Type != "VARCHAR" {
		t.Errorf("cols[1] = %+v; want name/VARCHAR", cols[1])
	}
	if cols[2].Name != "score" || cols[2].Type != "DOUBLE" {
		t.Errorf("cols[2] = %+v; want score/DOUBLE", cols[2])
	}
}

func TestTableRowCount(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	n, err := s.TableRowCount("people")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("TableRowCount(people) = %d, want 3", n)
	}
}

func TestTableSummaries(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	summaries, err := s.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	got := make(map[string]store.TableSummary, len(summaries))
	for _, s := range summaries {
		got[s.Name] = s
	}
	if got["people"].Rows != 3 || got["people"].Columns != 3 {
		t.Errorf("people summary = %+v; want rows=3 cols=3", got["people"])
	}
	if got["extras"].Rows != 2 || got["extras"].Columns != 2 {
		t.Errorf("extras summary = %+v; want rows=2 cols=2", got["extras"])
	}
	if got["recent_scores"].Rows != 2 || got["recent_scores"].Columns != 2 {
		t.Errorf("recent_scores summary = %+v; want rows=2 cols=2", got["recent_scores"])
	}
}

func TestIsRowEditable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// `people` has a PRIMARY KEY → editable.
	if !s.IsRowEditable("people") {
		t.Error("people should be row-editable (PK on id)")
	}
	// `extras` has no PK → not editable.
	if s.IsRowEditable("extras") {
		t.Error("extras has no PK; should not be row-editable")
	}
}

func TestQueryTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, nulls, ids, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if !reflect.DeepEqual(cols, []string{"id", "name", "score"}) {
		t.Errorf("cols = %v, want [id name score]", cols)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0][0] != "1" || rows[0][1] != "alice" {
		t.Errorf("row 0 = %v; want [1 alice 3.14]", rows[0])
	}
	// Carol's score is NULL.
	if !nulls[2][2] {
		t.Errorf("expected null flag for row 2 col 2 (carol.score)")
	}
	if ids[0] != 1 || ids[2] != 3 {
		t.Errorf("synthetic row IDs = %v, want [1 2 3]", ids)
	}
}

func TestQueryTableView(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, _, _, err := s.QueryTable("recent_scores")
	if err != nil {
		t.Fatalf("QueryTable(view): %v", err)
	}
	if !reflect.DeepEqual(cols, []string{"name", "score"}) {
		t.Errorf("cols = %v", cols)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2 (carol's null score is filtered)", len(rows))
	}
}

func TestReadOnlyQuery(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, err := s.ReadOnlyQuery("SELECT name FROM people WHERE score > 3")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if !reflect.DeepEqual(cols, []string{"name"}) {
		t.Errorf("cols = %v", cols)
	}
	if len(rows) != 1 || rows[0][0] != "alice" {
		t.Errorf("rows = %v; want [[alice]]", rows)
	}
}

func TestReadOnlyQueryRejectsWrites(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, _, err := s.ReadOnlyQuery("DELETE FROM people"); err == nil {
		t.Error("expected ReadOnlyQuery to reject DELETE")
	}
}

// TestMutationsStillStubbed asserts the PR-C-3b mutations that have not
// yet been bodied out still return store.ErrReadOnly. Each commit in
// PR-C-3a/3b peels off another entry here as the method is implemented.
func TestMutationsStillStubbed(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"ImportCSV", func() error { return s.ImportCSV("/tmp/none.csv", "x") }},
		{"AppendCSV", func() error { return s.AppendCSV("/tmp/none.csv", "x") }},
		{"ImportFile", func() error { return s.ImportFile("/tmp/none.csv", "x") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, store.ErrReadOnly) {
				t.Errorf("%s err = %v, want store.ErrReadOnly", tc.name, err)
			}
		})
	}
}

func TestRenameTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.RenameTable("people", "humans"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if slices.Contains(names, "people") {
		t.Errorf("people still present after rename: %v", names)
	}
	if !slices.Contains(names, "humans") {
		t.Errorf("humans not present after rename: %v", names)
	}
	// Cache for the old name should be cleared.
	if s.IsRowEditable("people") {
		t.Error("IsRowEditable(old name) = true; want false after rename")
	}
	n, err := s.TableRowCount("humans")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("humans rowcount = %d; want 3", n)
	}
}

func TestRenameTableRejectsUnsafeNames(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.RenameTable("people", `evil"; DROP TABLE people; --`); err == nil {
		t.Error("expected unsafe new name to be rejected")
	}
	if err := s.RenameTable(`evil"; DROP TABLE people; --`, "x"); err == nil {
		t.Error("expected unsafe old name to be rejected")
	}
}

func TestDropTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.DropTable("extras"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if slices.Contains(names, "extras") {
		t.Errorf("extras still present after drop: %v", names)
	}
}

func TestCreateEmptyTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.CreateEmptyTable("new_table"); err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if !slices.Contains(names, "new_table") {
		t.Errorf("new_table missing after create: %v", names)
	}
	// Has a PK → should be row-editable.
	if !s.IsRowEditable("new_table") {
		t.Error("new_table should be row-editable (id INTEGER PRIMARY KEY)")
	}
	// Schema columns match the SQLite default.
	cols, err := s.TableColumns("new_table")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	gotNames := lo.Map(cols, func(c store.PragmaColumn, _ int) string { return c.Name })
	if !reflect.DeepEqual(gotNames, []string{"id", "name", "value"}) {
		t.Errorf("columns = %v; want [id name value]", gotNames)
	}
}

func TestUpdateCell(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Prime the PK cache.
	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}

	// Alice → "ALICE".
	if err := s.UpdateCell("people", "name", 1, nil, ptr("ALICE")); err != nil {
		t.Fatalf("UpdateCell name: %v", err)
	}
	// Carol's score (currently NULL) → 9.9.
	if err := s.UpdateCell("people", "score", 3, nil, ptr("9.9")); err != nil {
		t.Fatalf("UpdateCell score: %v", err)
	}
	// Bob's score → NULL.
	if err := s.UpdateCell("people", "score", 2, nil, nil); err != nil {
		t.Fatalf("UpdateCell null: %v", err)
	}

	// Verify by re-querying.
	_, rows, nulls, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if rows[0][1] != "ALICE" {
		t.Errorf("alice name = %q; want ALICE", rows[0][1])
	}
	if rows[2][2] != "9.9" {
		t.Errorf("carol score = %q; want 9.9", rows[2][2])
	}
	if !nulls[1][2] {
		t.Errorf("bob score not NULL after update")
	}
}

func TestUpdateCellEscapesSingleQuote(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	// "O'Brien" needs the embedded quote to round-trip safely.
	if err := s.UpdateCell("people", "name", 1, nil, ptr("O'Brien")); err != nil {
		t.Fatalf("UpdateCell with quote: %v", err)
	}
	_, rows, _, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if rows[0][1] != "O'Brien" {
		t.Errorf("name = %q; want O'Brien", rows[0][1])
	}
}

func TestDeleteRows(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	// Delete alice (rowID 1) and bob (rowID 2); leave carol.
	n, err := s.DeleteRows("people", []store.RowIdentifier{{RowID: 1}, {RowID: 2}})
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2", n)
	}
	_, rows, _, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if len(rows) != 1 || rows[0][1] != "carol" {
		t.Errorf("remaining rows = %v; want [[3 carol …]]", rows)
	}
}

func TestDeleteRowsEmpty(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	n, err := s.DeleteRows("people", nil)
	if err != nil {
		t.Fatalf("DeleteRows(nil): %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d; want 0", n)
	}
}

func TestInsertRows(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Single-row insert with a value that contains a single quote so we
	// exercise sqlQuote round-tripping.
	err = s.InsertRows("people", []string{"id", "name", "score"}, [][]string{
		{"10", "O'Hara", "5.5"},
	})
	if err != nil {
		t.Fatalf("InsertRows: %v", err)
	}
	// Multi-row insert with one column omitted (empty string → NULL).
	err = s.InsertRows("people", []string{"id", "name", "score"}, [][]string{
		{"20", "ed", "1.0"},
		{"21", "fran", ""},
	})
	if err != nil {
		t.Fatalf("InsertRows multi: %v", err)
	}
	_, rows, nulls, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("row count = %d; want 6", len(rows))
	}
	// fran's score should be NULL (empty input → NULL per spec).
	for i, row := range rows {
		if row[0] == "21" && !nulls[i][2] {
			t.Errorf("fran's score not NULL after empty-string insert")
		}
		if row[0] == "10" && row[1] != "O'Hara" {
			t.Errorf("quoted name round-trip: got %q want O'Hara", row[1])
		}
	}
}

func TestInsertRowsEmpty(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.InsertRows("people", []string{"id"}, nil); err != nil {
		t.Fatalf("empty rows: %v", err)
	}
}

func TestInsertRowsIntoNoPKTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// No-PK tables still accept INSERT — only row-level addressing
	// requires a PK.
	if err := s.InsertRows("extras", []string{"k", "v"}, [][]string{{"c", "3"}}); err != nil {
		t.Fatalf("InsertRows into no-PK table: %v", err)
	}
	n, err := s.TableRowCount("extras")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("row count = %d; want 3", n)
	}
}

func TestDeleteRowsRejectsNoPKTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, _, _, _, err := s.QueryTable("extras"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if _, err := s.DeleteRows("extras", []store.RowIdentifier{{RowID: 1}}); err == nil {
		t.Errorf("expected error deleting from no-PK table")
	}
}

func TestUpdateCellRejectsNoPKTable(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, _, _, _, err := s.QueryTable("extras"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	// extras has no PK → UpdateCell should refuse.
	err = s.UpdateCell("extras", "v", 1, nil, ptr("42"))
	if err == nil {
		t.Fatalf("expected error for no-PK table")
	}
	if errors.Is(err, store.ErrReadOnly) {
		// Acceptable but not the most informative; we don't strictly
		// require ErrReadOnly here.
		return
	}
}

func TestUnsafeTableNameRejected(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.TableColumns(`evil"; DROP TABLE people; --`); err == nil {
		t.Error("expected unsafe table name to be rejected")
	}
}

func TestExportCSV(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	out := filepath.Join(t.TempDir(), "people.csv")
	if err := s.ExportCSV("people", out); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(b) == 0 || string(b[:3]) != "id," {
		t.Errorf("export contents = %q; want CSV with header", string(b))
	}
}

// ptr is a one-line generic pointer helper used by the table-driven
// mutation tests above.
func ptr[T any](v T) *T { return &v }
