package duck_test

// store_test.go — integration tests against a real duckdb subprocess.
// Each test generates its own fixture .duckdb via the `duckdb` CLI so we
// don't have to commit binary blobs. Tests skip cleanly when duckdb is
// not on PATH.
//
// Shared DataStore-iface assertions live in internal/store/contracttest
// and are wired up via [TestStoreContract] in contract_test.go. The tests
// here cover duck-specific behaviour only: subprocess lifecycle, view
// detection, PK-less table rejections, heavy-type placeholders, the
// rowKeys cache invalidation on rename, and quoting round-trips.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

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

// ---------- subprocess lifecycle ----------

func TestOpenAndClose(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// We can't easily un-install duckdb; instead, point the path at a
	// non-existent file and check the resulting error doesn't crash.
	requireDuck(t)
	_, err := duck.Open(filepath.Join(t.TempDir(), "does-not-exist.duckdb"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

// ---------- view detection (duck-specific: VIEW vs TABLE in information_schema) ----------

func TestTableNamesAndViews(t *testing.T) {
	t.Parallel()
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

// TestTableColumns asserts duck-specific column types (BIGINT/VARCHAR/DOUBLE)
// surface via the information_schema-backed describeColumns path.
func TestTableColumns(t *testing.T) {
	t.Parallel()
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

// ---------- IsRowEditable (duck-specific: PK presence gates row mutations) ----------

func TestIsRowEditable(t *testing.T) {
	t.Parallel()
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
	// CreateEmptyTable produces a table with a PK → editable.
	if err := s.CreateEmptyTable("fresh"); err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}
	if !s.IsRowEditable("fresh") {
		t.Error("freshly created empty table should be row-editable (id INTEGER PRIMARY KEY)")
	}
}

// ---------- view querying (duck-specific code path) ----------

func TestQueryTableView(t *testing.T) {
	t.Parallel()
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

// TestReadOnlyQueryWithUserLimit guards against the row-cap LIMIT being
// naively concatenated onto a user query that already terminates in LIMIT
// or ORDER BY — naive `query + " LIMIT 200"` produces `SELECT … LIMIT 1
// LIMIT 200`, which duckdb rejects as a parse error.
func TestReadOnlyQueryWithUserLimit(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, rows, err := s.ReadOnlyQuery("SELECT name FROM people ORDER BY score DESC LIMIT 1")
	if err != nil {
		t.Fatalf("ReadOnlyQuery with user LIMIT: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("got %d rows, want 1 (user LIMIT 1 should be honored)", len(rows))
	}
}

// ---------- rename: duck-specific rowEditable cache invalidation ----------

func TestRenameTable(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Prime caches against the old name so we can verify invalidation.
	if !s.IsRowEditable("people") {
		t.Fatal("precondition: people should be row-editable before rename")
	}

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
	n, err := s.TableRowCount("humans")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("humans rowcount = %d; want 3", n)
	}
	// The renamed-away cache entry should be gone: IsRowEditable for the
	// new name primes fresh, while the old-name entry was wiped.
	if !s.IsRowEditable("humans") {
		t.Error("IsRowEditable(humans) = false; want true after rename")
	}
}

// ---------- mutations: duck-specific quoting + transitions ----------

// TestUpdateCell exercises duck's PK-cache resolution and the NULL→value
// transition (carol's NULL score → 9.9) that the contract test doesn't
// cover.
func TestUpdateCell(t *testing.T) {
	t.Parallel()
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

	// Carol's score (currently NULL) → 9.9.
	if err := s.UpdateCell("people", "score", 3, nil, new("9.9")); err != nil {
		t.Fatalf("UpdateCell score: %v", err)
	}
	// Bob's score → NULL.
	if err := s.UpdateCell("people", "score", 2, nil, nil); err != nil {
		t.Fatalf("UpdateCell null: %v", err)
	}

	_, rows, nulls, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if rows[2][2] != "9.9" {
		t.Errorf("carol score = %q; want 9.9", rows[2][2])
	}
	if !nulls[1][2] {
		t.Errorf("bob score not NULL after update")
	}
}

func TestUpdateCellEscapesSingleQuote(t *testing.T) {
	t.Parallel()
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
	if err := s.UpdateCell("people", "name", 1, nil, new("O'Brien")); err != nil {
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

// TestInsertRows asserts duck-specific quote round-tripping and the
// empty-string→NULL semantics during multi-row INSERT.
func TestInsertRows(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	err = s.UpdateCell("extras", "v", 1, nil, new("42"))
	if err == nil {
		t.Fatalf("expected error for no-PK table")
	}
	if errors.Is(err, store.ErrReadOnly) {
		// Acceptable but not the most informative; we don't strictly
		// require ErrReadOnly here.
		return
	}
}

// ---------- export / import (duck-specific behaviours) ----------

func TestExportCSV(t *testing.T) {
	t.Parallel()
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

func TestImportCSVQuotedPath(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Filename with a single quote — exercises sqlQuote on the path.
	dir := t.TempDir()
	path := filepath.Join(dir, "o'data.csv")
	if err := os.WriteFile(path, []byte("a\n1\n2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.ImportCSV(path, "quoted"); err != nil {
		t.Fatalf("ImportCSV with quoted path: %v", err)
	}
	n, err := s.TableRowCount("quoted")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 2 {
		t.Errorf("rowcount = %d; want 2", n)
	}
}

// TestImportFileDuckFormats covers the import formats that the duck
// backend supports beyond the contract suite's csv/json/jsonl (TSV and
// NDJSON, both routed via `read_csv_auto` / `read_json_auto`).
func TestImportFileDuckFormats(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	cases := []struct {
		name     string
		ext      string
		contents string
		table    string
		wantRows int
	}{
		{
			name:     "tsv",
			ext:      ".tsv",
			contents: "k\tv\na\t1\nb\t2\nc\t3\n",
			table:    "from_tsv",
			wantRows: 3,
		},
		{
			name:     "ndjson",
			ext:      ".ndjson",
			contents: "{\"k\":\"a\",\"v\":1}\n{\"k\":\"b\",\"v\":2}\n",
			table:    "from_ndjson",
			wantRows: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := duck.Open(makeFixture(t))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = s.Close() }()

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

// makeParquetFixture writes a small Parquet file (the columnar binary
// format the pure-Go SQLite path can't parse) via duckdb's COPY ... TO
// and returns its path. The table is metrics(id, name, score) with three
// rows, one NULL score.
func makeParquetFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metrics.parquet")
	script := fmt.Sprintf(
		`COPY (SELECT * FROM (VALUES (1, 'alice', 3.14), (2, 'bob', 2.72), (3, 'carol', NULL)) AS t(id, name, score)) TO '%s' (FORMAT PARQUET);`,
		path)
	cmd := exec.Command("duckdb", ":memory:")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create parquet fixture: %v\n%s", err, out)
	}
	return path
}

// TestOpenFileViewParquet covers viewing a Parquet file: it opens through
// an in-memory duckdb subprocess as a read-only VIEW named after the file
// stem, with its rows queryable.
func TestOpenFileViewParquet(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.OpenFileView(makeParquetFixture(t))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if want := []string{"metrics"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("TableNames = %v, want %v", names, want)
	}
	if !s.IsView("metrics") {
		t.Error("IsView(metrics) = false; want true (parquet file view is read-only)")
	}
	n, err := s.TableRowCount("metrics")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("rowcount = %d; want 3", n)
	}
}

// TestOpenFileViewRejectsUnsupported asserts OpenFileView returns
// ErrImportNotSupported for a format duckdb's reader dispatch doesn't
// cover (here .xlsx, which the duck store import path doesn't handle).
func TestOpenFileViewRejectsUnsupported(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	path := writeTempFile(t, ".xlsx", "not really a spreadsheet")
	_, err := duck.OpenFileView(path)
	if !errors.Is(err, store.ErrImportNotSupported) {
		t.Fatalf("OpenFileView(.xlsx) error = %v; want ErrImportNotSupported", err)
	}
}

// TestImportFileParquet covers importing a Parquet file as a new table in
// an existing duckdb store via the read_parquet reader.
func TestImportFileParquet(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.ImportFile(makeParquetFixture(t), "from_parquet"); err != nil {
		t.Fatalf("ImportFile(parquet): %v", err)
	}
	n, err := s.TableRowCount("from_parquet")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if n != 3 {
		t.Errorf("rowcount = %d; want 3", n)
	}
}

// writeTempFile writes contents to a fresh file under t.TempDir() with
// the given extension and returns the absolute path.
func writeTempFile(t *testing.T, ext, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data"+ext)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// makeHeavyFixture writes a .duckdb file exercising the heavy-type
// projection path: a FLOAT[] array column, a STRUCT, a BLOB, and a JSON
// column. Two rows with one NULL each so the NULL-pass-through is
// observable.
func makeHeavyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heavy.duckdb")
	script := `CREATE TABLE vecs (
  id BIGINT PRIMARY KEY,
  label VARCHAR,
  embedding FLOAT[],
  info STRUCT(name VARCHAR, score DOUBLE),
  payload BLOB,
  meta JSON
);
INSERT INTO vecs VALUES
  (1, 'a', [0.1, 0.2, 0.3, 0.4]::FLOAT[], {'name': 'alice', 'score': 3.14}, 'hello'::BLOB, '{"k":1}'),
  (2, 'b', NULL, {'name': 'bob', 'score': 2.72}, NULL, '{"k":2,"nested":{"x":1}}');
`
	cmd := exec.Command("duckdb", path)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create heavy fixture: %v\n%s", err, out)
	}
	return path
}

// TestQueryTableHeavyTypesEmitPlaceholders verifies the SELECT projection
// rewrite — heavy columns come back as short typed placeholders instead
// of the full JSON-serialised payload.
func TestQueryTableHeavyTypesEmitPlaceholders(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeHeavyFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, nulls, _, err := s.QueryTable("vecs")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	want := []string{"id", "label", "embedding", "info", "payload", "meta"}
	if !reflect.DeepEqual(cols, want) {
		t.Fatalf("cols = %v, want %v", cols, want)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	// Row 0: full values.
	checks := map[int]string{
		2: "<FLOAT[4]>",
		3: "<STRUCT>",
		4: "<BLOB 5 bytes>",
	}
	for ci, want := range checks {
		if rows[0][ci] != want {
			t.Errorf("row 0 col %d (%s) = %q, want %q", ci, cols[ci], rows[0][ci], want)
		}
	}
	// JSON placeholder includes the char count — we just sanity-check the prefix.
	if !strings.HasPrefix(rows[0][5], "<JSON ") || !strings.HasSuffix(rows[0][5], " chars>") {
		t.Errorf("row 0 meta placeholder = %q; want <JSON N chars>", rows[0][5])
	}

	// Row 1: embedding and payload are NULL → null flags set, no placeholder.
	if !nulls[1][2] {
		t.Errorf("row 1 embedding should be NULL")
	}
	if rows[1][2] != "" {
		t.Errorf("row 1 embedding value = %q; want empty for NULL", rows[1][2])
	}
	if !nulls[1][4] {
		t.Errorf("row 1 payload should be NULL")
	}

	// IsHeavyColumn caches the column set.
	for _, c := range []string{"embedding", "info", "payload", "meta"} {
		if !s.IsHeavyColumn("vecs", c) {
			t.Errorf("IsHeavyColumn(vecs, %s) = false; want true", c)
		}
	}
	for _, c := range []string{"id", "label"} {
		if s.IsHeavyColumn("vecs", c) {
			t.Errorf("IsHeavyColumn(vecs, %s) = true; want false", c)
		}
	}
}

// TestFetchCellRoundTrip verifies that FetchCell returns the *full* value
// of a heavy cell (the placeholder shown in the table) by resolving the
// synthetic rowID back to the PK via the rowKeys cache.
func TestFetchCellRoundTrip(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeHeavyFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Prime the rowKeys cache.
	if _, _, _, _, err := s.QueryTable("vecs"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}

	// Embedding: rowID=1 → full FLOAT[4] payload.
	got, isNull, err := s.FetchCell("vecs", "embedding", 1)
	if err != nil {
		t.Fatalf("FetchCell embedding: %v", err)
	}
	if isNull {
		t.Fatalf("FetchCell embedding row 1 returned null")
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("FetchCell embedding = %q; want JSON array", got)
	}
	for _, want := range []string{"0.1", "0.2", "0.3", "0.4"} {
		if !strings.Contains(got, want) {
			t.Errorf("FetchCell embedding = %q; missing %q", got, want)
		}
	}

	// Embedding: rowID=2 is NULL → isNull=true.
	_, isNull, err = s.FetchCell("vecs", "embedding", 2)
	if err != nil {
		t.Fatalf("FetchCell embedding row 2: %v", err)
	}
	if !isNull {
		t.Errorf("FetchCell embedding row 2 should be null")
	}

	// Struct value: well-formed JSON object with the original keys.
	got, _, err = s.FetchCell("vecs", "info", 1)
	if err != nil {
		t.Fatalf("FetchCell info: %v", err)
	}
	if !strings.Contains(got, "alice") || !strings.Contains(got, "3.14") {
		t.Errorf("FetchCell info = %q; want struct with alice/3.14", got)
	}
}

// TestFetchCellNoPKReturnsError exercises the contract for PK-less tables:
// FetchCell needs cached PK values built by QueryTable, and a table
// without a PK leaves rowKeys empty.
func TestFetchCellNoPKReturnsError(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// extras has no PK → rowKeys cache stays empty for it.
	if _, _, _, _, err := s.QueryTable("extras"); err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if _, _, err := s.FetchCell("extras", "v", 1); err == nil {
		t.Error("expected FetchCell to error for PK-less table")
	}
}

// TestQueryTableNonHeavyUntouched verifies the placeholder rewrite is a
// no-op on schemas without any heavy columns — the existing fixture's
// people table still returns numeric / string values verbatim.
func TestQueryTableNonHeavyUntouched(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	s, err := duck.Open(makeFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, rows, _, _, err := s.QueryTable("people")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if rows[0][1] != "alice" {
		t.Errorf("row 0 name = %q, want alice", rows[0][1])
	}
	if rows[0][2] != "3.14" {
		t.Errorf("row 0 score = %q, want 3.14", rows[0][2])
	}
	if s.IsHeavyColumn("people", "name") {
		t.Errorf("IsHeavyColumn(people, name) = true; want false")
	}
}
