// Package contracttest provides a shared test driver that exercises the
// store.DataStore interface contract against any backend. Both the sqlite
// and duck backends import this and call Run(t, setupFunc) in their own
// TestStoreContract so the same iface assertions run against both.
//
// SetupFunc creates a fresh DataStore populated with the contract fixture:
//
//	people(id INT PRIMARY KEY, name TEXT, score REAL)
//	  (1, 'alice', 3.14)
//	  (2, 'bob',   2.72)
//	  (3, 'carol', NULL)
//
//	extras(k TEXT, v INT)
//	  ('a', 1)
//	  ('b', 2)
//
// Each subtest receives its own fresh fixture from setup; mutating
// subtests do not interfere with each other.
package contracttest

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// SetupFunc returns a fresh DataStore populated with the contract fixture.
// Implementations are responsible for cleanup via t.Cleanup.
type SetupFunc func(t *testing.T) store.DataStore

// Run executes the full DataStore contract suite against the backend
// produced by setup. Each subtest invokes setup again for isolation.
func Run(t *testing.T, setup SetupFunc) {
	t.Helper()
	t.Run("TableNames", func(t *testing.T) { testTableNames(t, setup) })
	t.Run("TableColumns", func(t *testing.T) { testTableColumns(t, setup) })
	t.Run("TableRowCount", func(t *testing.T) { testTableRowCount(t, setup) })
	t.Run("TableSummaries", func(t *testing.T) { testTableSummaries(t, setup) })
	t.Run("QueryTable", func(t *testing.T) { testQueryTable(t, setup) })
	t.Run("ReadOnlyQuery", func(t *testing.T) { testReadOnlyQuery(t, setup) })
	t.Run("ReadOnlyQueryRejectsWrites", func(t *testing.T) { testReadOnlyQueryRejectsWrites(t, setup) })
	t.Run("UpdateCell", func(t *testing.T) { testUpdateCell(t, setup) })
	t.Run("UpdateCellNull", func(t *testing.T) { testUpdateCellNull(t, setup) })
	t.Run("DeleteRows", func(t *testing.T) { testDeleteRows(t, setup) })
	t.Run("DeleteRowsEmpty", func(t *testing.T) { testDeleteRowsEmpty(t, setup) })
	t.Run("InsertRows", func(t *testing.T) { testInsertRows(t, setup) })
	t.Run("RenameTable", func(t *testing.T) { testRenameTable(t, setup) })
	t.Run("RenameTableRejectsUnsafe", func(t *testing.T) { testRenameTableRejectsUnsafe(t, setup) })
	t.Run("DropTable", func(t *testing.T) { testDropTable(t, setup) })
	t.Run("CreateEmptyTable", func(t *testing.T) { testCreateEmptyTable(t, setup) })
	// CreateEmptyTableExisting (asserting CREATE on a duplicate name errors)
	// is not in the contract suite: duck's stderr-drain goroutine races
	// against the sentinel-stdout read for errors raised by the failing
	// statement, so the error is reliably propagated only when stderr has
	// drained — see subproc.go. Each backend keeps its own variant.
	t.Run("ImportCSV", func(t *testing.T) { testImportCSV(t, setup) })
	t.Run("AppendCSV", func(t *testing.T) { testAppendCSV(t, setup) })
	t.Run("AppendCSVMissingTable", func(t *testing.T) { testAppendCSVMissingTable(t, setup) })
	t.Run("ImportFile", func(t *testing.T) { testImportFile(t, setup) })
	t.Run("ImportFileUnsupportedExt", func(t *testing.T) { testImportFileUnsupportedExt(t, setup) })
	t.Run("UnsafeTableNameRejected", func(t *testing.T) { testUnsafeTableNameRejected(t, setup) })
}

// ---------- subtests ----------

func testTableNames(t *testing.T, setup SetupFunc) {
	s := setup(t)
	got, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	// Backends differ on default ordering; sort before comparing.
	slices.Sort(got)
	want := []string{"extras", "people"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TableNames = %v, want %v", got, want)
	}
}

func testTableColumns(t *testing.T, setup SetupFunc) {
	s := setup(t)
	cols, err := s.TableColumns("people")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(cols) = %d, want 3", len(cols))
	}
	if cols[0].Name != "id" || cols[0].PK != 1 {
		t.Errorf("cols[0] = %+v; want id with PK=1", cols[0])
	}
	if cols[1].Name != "name" || cols[1].PK != 0 {
		t.Errorf("cols[1] = %+v; want name with PK=0", cols[1])
	}
	if cols[2].Name != "score" || cols[2].PK != 0 {
		t.Errorf("cols[2] = %+v; want score with PK=0", cols[2])
	}
}

func testTableRowCount(t *testing.T, setup SetupFunc) {
	s := setup(t)
	n, err := s.TableRowCount("people")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("TableRowCount(people) = %d; want 3", n)
	}
}

func testTableSummaries(t *testing.T, setup SetupFunc) {
	s := setup(t)
	summaries, err := s.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	got := lo.SliceToMap(summaries, func(s store.TableSummary) (string, store.TableSummary) {
		return s.Name, s
	})
	if got["people"].Rows != 3 || got["people"].Columns != 3 {
		t.Errorf("people summary = %+v; want rows=3 cols=3", got["people"])
	}
	if got["extras"].Rows != 2 || got["extras"].Columns != 2 {
		t.Errorf("extras summary = %+v; want rows=2 cols=2", got["extras"])
	}
}

func testQueryTable(t *testing.T, setup SetupFunc) {
	s := setup(t)
	cols, rows, nulls, ids, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if !reflect.DeepEqual(cols, []string{"id", "name", "score"}) {
		t.Errorf("cols = %v; want [id name score]", cols)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// rowIDs match the PK values because id is the PK (sqlite rowid alias
	// and duck synthetic id derive equally from id=1,2,3).
	if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Errorf("rowIDs = %v; want [1 2 3]", ids)
	}
	if rows[0][1] != "alice" {
		t.Errorf("row 0 name = %q; want alice", rows[0][1])
	}
	// Carol's score is NULL → null flag set, value empty.
	if !nulls[2][2] {
		t.Error("nulls[2][2] (carol.score) = false; want true")
	}
	if rows[2][2] != "" {
		t.Errorf("rows[2][2] = %q; want empty (NULL)", rows[2][2])
	}
}

func testReadOnlyQuery(t *testing.T, setup SetupFunc) {
	s := setup(t)
	cols, rows, err := s.ReadOnlyQuery("SELECT name FROM people WHERE score > 3")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if !reflect.DeepEqual(cols, []string{"name"}) {
		t.Errorf("cols = %v; want [name]", cols)
	}
	if len(rows) != 1 || rows[0][0] != "alice" {
		t.Errorf("rows = %v; want [[alice]]", rows)
	}
}

func testReadOnlyQueryRejectsWrites(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if _, _, err := s.ReadOnlyQuery("DELETE FROM people"); err == nil {
		t.Error("expected error for DELETE")
	}
	if _, _, err := s.ReadOnlyQuery("INSERT INTO people (name) VALUES ('evil')"); err == nil {
		t.Error("expected error for INSERT")
	}
}

func testUpdateCell(t *testing.T, setup SetupFunc) {
	s := setup(t)
	// Prime caches (duck needs QueryTable before mutations).
	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	newName := "ALICE"
	if err := s.UpdateCell("people", "name", 1, nil, &newName); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}
	_, rows, _, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if rows[0][1] != "ALICE" {
		t.Errorf("name after update = %q; want ALICE", rows[0][1])
	}
}

func testUpdateCellNull(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	// Set alice's score to NULL.
	if err := s.UpdateCell("people", "score", 1, nil, nil); err != nil {
		t.Fatalf("UpdateCell(nil): %v", err)
	}
	_, _, nulls, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if !nulls[0][2] {
		t.Error("alice.score not NULL after UpdateCell(nil)")
	}
}

func testDeleteRows(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if _, _, _, _, err := s.QueryTable("people"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	n, err := s.DeleteRows("people", []store.RowIdentifier{{RowID: 1}, {RowID: 2}})
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2", n)
	}
	count, err := s.TableRowCount("people")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 1 {
		t.Errorf("post-delete row count = %d; want 1", count)
	}
}

func testDeleteRowsEmpty(t *testing.T, setup SetupFunc) {
	s := setup(t)
	n, err := s.DeleteRows("people", nil)
	if err != nil {
		t.Fatalf("DeleteRows(nil): %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d; want 0", n)
	}
}

func testInsertRows(t *testing.T, setup SetupFunc) {
	s := setup(t)
	cols := []string{"id", "name", "score"}
	rows := [][]string{
		{"10", "dave", "1.5"},
		{"11", "ed", ""}, // empty string → NULL per spec
	}
	if err := s.InsertRows("people", cols, rows); err != nil {
		t.Fatalf("InsertRows: %v", err)
	}
	count, err := s.TableRowCount("people")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 5 {
		t.Errorf("row count after insert = %d; want 5", count)
	}
	_, gotRows, nulls, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	for i, r := range gotRows {
		if r[0] == "11" && !nulls[i][2] {
			t.Errorf("ed's score not NULL after empty-string insert: %v", r)
		}
	}
}

func testRenameTable(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if err := s.RenameTable("people", "humans"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if slices.Contains(names, "people") {
		t.Error("people still present after rename")
	}
	if !slices.Contains(names, "humans") {
		t.Error("humans missing after rename")
	}
	n, err := s.TableRowCount("humans")
	if err != nil {
		t.Fatalf("TableRowCount(humans): %v", err)
	}
	if n != 3 {
		t.Errorf("humans rowcount = %d; want 3", n)
	}
}

func testRenameTableRejectsUnsafe(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if err := s.RenameTable("people", `evil"; DROP TABLE people; --`); err == nil {
		t.Error("expected error for unsafe new name")
	}
	if err := s.RenameTable(`evil"; DROP TABLE x; --`, "ok"); err == nil {
		t.Error("expected error for unsafe old name")
	}
}

func testDropTable(t *testing.T, setup SetupFunc) {
	s := setup(t)
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

func testCreateEmptyTable(t *testing.T, setup SetupFunc) {
	s := setup(t)
	if err := s.CreateEmptyTable("new_table"); err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}
	count, err := s.TableRowCount("new_table")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 0 {
		t.Errorf("new table row count = %d; want 0", count)
	}
	cols, err := s.TableColumns("new_table")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Errorf("new_table columns = %d; want 3 (id, name, value)", len(cols))
	}
	wantNames := []string{"id", "name", "value"}
	for i, want := range wantNames {
		if i >= len(cols) || cols[i].Name != want {
			t.Errorf("col[%d] = %v; want %s", i, cols[i], want)
		}
	}
}

func writeTempFile(t *testing.T, ext, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data"+ext)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func testImportCSV(t *testing.T, setup SetupFunc) {
	s := setup(t)
	csv := writeTempFile(t, ".csv", "k,v\nx,1\ny,2\nz,3\n")
	if err := s.ImportCSV(csv, "imported"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	n, err := s.TableRowCount("imported")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("rowcount = %d; want 3", n)
	}
	cols, err := s.TableColumns("imported")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "k" || cols[1].Name != "v" {
		t.Errorf("cols = %+v; want [k v]", cols)
	}
}

func testAppendCSV(t *testing.T, setup SetupFunc) {
	s := setup(t)
	csv := writeTempFile(t, ".csv", "k,v\nc,3\nd,4\n")
	if err := s.AppendCSV(csv, "extras"); err != nil {
		t.Fatalf("AppendCSV: %v", err)
	}
	n, err := s.TableRowCount("extras")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 4 {
		t.Errorf("rowcount after append = %d; want 4", n)
	}
}

func testAppendCSVMissingTable(t *testing.T, setup SetupFunc) {
	s := setup(t)
	csv := writeTempFile(t, ".csv", "k,v\nc,3\n")
	if err := s.AppendCSV(csv, "no_such_table"); err == nil {
		t.Error("expected error appending to missing table")
	}
}

func testImportFile(t *testing.T, setup SetupFunc) {
	cases := []struct {
		name     string
		ext      string
		contents string
		table    string
		wantRows int
	}{
		{"csv", ".csv", "k,v\na,1\nb,2\n", "from_csv", 2},
		{"json", ".json", `[{"k":"a","v":1},{"k":"b","v":2}]`, "from_json", 2},
		{"jsonl", ".jsonl", "{\"k\":\"a\",\"v\":1}\n{\"k\":\"b\",\"v\":2}\n", "from_jsonl", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setup(t)
			path := writeTempFile(t, tc.ext, tc.contents)
			if err := s.ImportFile(path, tc.table); err != nil {
				t.Fatalf("ImportFile(%s): %v", tc.ext, err)
			}
			n, err := s.TableRowCount(tc.table)
			if err != nil {
				t.Fatalf("TableRowCount(%s): %v", tc.table, err)
			}
			if n != tc.wantRows {
				t.Errorf("rowcount = %d; want %d", n, tc.wantRows)
			}
		})
	}
}

func testImportFileUnsupportedExt(t *testing.T, setup SetupFunc) {
	s := setup(t)
	path := writeTempFile(t, ".xyz", "anything")
	err := s.ImportFile(path, "bad")
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	// Both backends should surface either ErrImportNotSupported or an
	// error message that mentions "unsupported".
	if !errors.Is(err, store.ErrImportNotSupported) && !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("err = %v; want ErrImportNotSupported or 'unsupported' in message", err)
	}
}

func testUnsafeTableNameRejected(t *testing.T, setup SetupFunc) {
	s := setup(t)
	bad := `evil"; DROP TABLE people; --`
	if _, err := s.TableColumns(bad); err == nil {
		t.Error("TableColumns should reject unsafe name")
	}
	if _, err := s.TableRowCount(bad); err == nil {
		t.Error("TableRowCount should reject unsafe name")
	}
	if err := s.DropTable(bad); err == nil {
		t.Error("DropTable should reject unsafe name")
	}
	if _, err := s.DeleteRows(bad, []store.RowIdentifier{{RowID: 1}}); err == nil {
		t.Error("DeleteRows should reject unsafe name")
	}
	if err := s.InsertRows(bad, []string{"a"}, [][]string{{"1"}}); err == nil {
		t.Error("InsertRows should reject unsafe name")
	}
}
