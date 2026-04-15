package view

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	_ "modernc.org/sqlite"

	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/zot/local"
)

func TestStoreTableNames(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	names, err := store.TableNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != TableName {
		t.Fatalf("TableNames = %v, want [%s]", names, TableName)
	}
}

func TestStoreQueryItemsTable(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	cols, rows, nullFlags, rowIDs, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}

	wantCols := []string{
		"Author(s)",
		"Year",
		"Journal/Publication",
		"Title",
		"Date Added",
		"Extra",
		"Notes",
		"PDF",
	}
	if !sliceEqual(cols, wantCols) {
		t.Fatalf("columns = %v, want %v", cols, wantCols)
	}

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (attachments, notes, trashed items must be filtered)", len(rows))
	}
	if len(nullFlags) != len(rows) || len(rowIDs) != len(rows) {
		t.Fatalf("nullFlags/rowIDs length mismatch: rows=%d nullFlags=%d rowIDs=%d",
			len(rows), len(nullFlags), len(rowIDs))
	}

	// Row 0 — item 10, most recent dateAdded, 2 authors, journalArticle, has docling note, has PDF.
	wantRow0 := []string{
		"Smith, Alice; Jones, Bob",
		"2024",
		"NeuroImage",
		"Transformers in fMRI Analysis",
		"03/15/24, 10:00am",
		"Citation Key: xyz",
		"Extracted",
		"Yes",
	}
	if !sliceEqual(rows[0], wantRow0) {
		t.Errorf("row 0 = %v,\nwant    %v", rows[0], wantRow0)
	}
	if rowIDs[0] != 10 {
		t.Errorf("row 0 rowID = %d, want 10", rowIDs[0])
	}

	// Row 1 — item 20, institutional author, year-only date, no journal, no note, no PDF.
	wantRow1 := []string{
		"NASA",
		"2023",
		"",
		"Deep Space Report",
		"02/01/24, 10:00am",
		"Citation Key: abc",
		"-",
		"-",
	}
	if !sliceEqual(rows[1], wantRow1) {
		t.Errorf("row 1 = %v,\nwant    %v", rows[1], wantRow1)
	}

	// Row 2 — item 30, book, no date/journal/extra, no note, no PDF.
	wantRow2 := []string{
		"Curie, Marie",
		"",
		"",
		"A Book About Radium",
		"01/01/24, 10:00am",
		"",
		"-",
		"-",
	}
	if !sliceEqual(rows[2], wantRow2) {
		t.Errorf("row 2 = %v,\nwant    %v", rows[2], wantRow2)
	}
}

func TestStoreEditorsExcludedFromAuthors(t *testing.T) {
	// Item 10 has Eve Editor as creatorType='editor'; she must not appear
	// in the Author(s) column. Guarded implicitly by TestStoreQueryItemsTable,
	// but called out here so a regression is easy to diagnose.
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	_, rows, _, _, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0][0]; got == "" || contains(got, "Editor") {
		t.Errorf("row 0 authors = %q, editor must be excluded", got)
	}
}

func TestStoreTableRowCount(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	n, err := store.TableRowCount(TableName)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("TableRowCount = %d, want 3", n)
	}
}

func TestStoreTableSummaries(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	sums, err := store.TableSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 {
		t.Fatalf("TableSummaries = %d entries, want 1", len(sums))
	}
	if sums[0].Name != TableName || sums[0].Rows != 3 || sums[0].Columns != 8 {
		t.Errorf("summary = %+v, want {items 3 8}", sums[0])
	}
}

func TestStoreIsView(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	var vl data.ViewLister = store // compile-time: Store must implement ViewLister
	if !vl.IsView(TableName) {
		t.Errorf("IsView(%q) = false, want true — dbtui uses this to force read-only", TableName)
	}
	if vl.IsView("something_else") {
		t.Errorf("IsView(something_else) = true, should only match items")
	}
}

func TestStoreWriteMethodsBlocked(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	checks := []struct {
		name string
		run  func() error
	}{
		{"UpdateCell", func() error { return store.UpdateCell(TableName, "Title", 1, nil, nil) }},
		{"DeleteRows", func() error { _, err := store.DeleteRows(TableName, nil); return err }},
		{"InsertRows", func() error { return store.InsertRows(TableName, nil, nil) }},
		{"RenameTable", func() error { return store.RenameTable(TableName, "x") }},
		{"DropTable", func() error { return store.DropTable(TableName) }},
		{"CreateEmptyTable", func() error { return store.CreateEmptyTable("x") }},
		{"ExportCSV", func() error { return store.ExportCSV(TableName, "/tmp/x.csv") }},
	}
	for _, c := range checks {
		if err := c.run(); err == nil || !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: err = %v, want ErrReadOnly", c.name, err)
		}
	}
}

// newTestStore builds a fresh fixture directory and returns a Store fixed to
// UTC so Date Added formatting is deterministic regardless of host timezone.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := seedViewFixture(t)
	db, err := local.Open(dir)
	if err != nil {
		t.Fatalf("local.Open: %v", err)
	}
	return New(db, time.UTC)
}

// seedViewFixture writes a minimal zotero.sqlite tailored to the view tests.
// It intentionally duplicates a small subset of local/fixture_test.go's schema
// because that fixture lives in a _test.go file in another package and cannot
// be imported from here. Keep this synchronised with any new columns the
// view layer starts reading from the DB.
func seedViewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "zotero.sqlite")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ddl := []string{
		`CREATE TABLE version (schema TEXT PRIMARY KEY, version INTEGER)`,
		`CREATE TABLE libraries (libraryID INTEGER PRIMARY KEY, type TEXT)`,
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT UNIQUE)`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT UNIQUE)`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT UNIQUE)`,
		`CREATE TABLE items (
			itemID INTEGER PRIMARY KEY,
			itemTypeID INTEGER,
			libraryID INTEGER,
			key TEXT,
			dateAdded TEXT,
			dateModified TEXT,
			clientDateModified TEXT
		)`,
		`CREATE TABLE itemData (itemID INTEGER, fieldID INTEGER, valueID INTEGER, PRIMARY KEY (itemID, fieldID))`,
		`CREATE TABLE deletedItems (itemID INTEGER PRIMARY KEY)`,
		`CREATE TABLE creators (creatorID INTEGER PRIMARY KEY, firstName TEXT, lastName TEXT, fieldMode INTEGER)`,
		`CREATE TABLE creatorTypes (creatorTypeID INTEGER PRIMARY KEY, creatorType TEXT)`,
		`CREATE TABLE itemCreators (itemID INTEGER, creatorID INTEGER, creatorTypeID INTEGER, orderIndex INTEGER, PRIMARY KEY (itemID, orderIndex))`,
		`CREATE TABLE itemNotes (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, note TEXT, title TEXT)`,
		`CREATE TABLE tags (tagID INTEGER PRIMARY KEY, name TEXT UNIQUE, type INTEGER)`,
		`CREATE TABLE itemTags (itemID INTEGER, tagID INTEGER, type INTEGER, PRIMARY KEY (itemID, tagID))`,
		`CREATE TABLE itemAttachments (itemID INTEGER PRIMARY KEY, parentItemID INT, linkMode INT, contentType TEXT)`,
	}
	for _, s := range ddl {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("ddl %q: %v", s, err)
		}
	}

	seed := []string{
		`INSERT INTO version VALUES ('userdata', 125)`,
		`INSERT INTO libraries VALUES (1, 'user')`,

		`INSERT INTO itemTypes VALUES
			(1,'journalArticle'),
			(2,'book'),
			(3,'attachment'),
			(4,'note'),
			(5,'report')`,

		`INSERT INTO fields VALUES
			(1,'title'),
			(2,'date'),
			(3,'publicationTitle'),
			(4,'extra')`,

		`INSERT INTO creatorTypes VALUES (1,'author'),(2,'editor')`,

		`INSERT INTO creators VALUES
			(1,'Alice','Smith',0),
			(2,'Bob','Jones',0),
			(3,'','NASA',1),
			(4,'Marie','Curie',0),
			(5,'Eve','Editor',0)`,

		// Items:
		//   10 — journalArticle, content, full metadata, 2 authors + 1 editor
		//   20 — report, content, institutional author, year-only date
		//   30 — book, content, single author, missing date/pub/extra
		//   40 — attachment (must be excluded)
		//   50 — note (must be excluded)
		//   60 — journalArticle but trashed (must be excluded)
		// dateAdded uses ISO-8601 with trailing Z — that's what current
		// Zotero releases write. Item 30 is intentionally left on the older
		// space-separated form so the store's dual-layout parser stays
		// exercised.
		`INSERT INTO items VALUES
			(10, 1, 1, 'IT10', '2024-03-15T10:00:00Z', '', ''),
			(20, 5, 1, 'IT20', '2024-02-01T10:00:00Z', '', ''),
			(30, 2, 1, 'IT30', '2024-01-01 10:00:00',  '', ''),
			(40, 3, 1, 'ATT',  '2024-03-20T10:00:00Z', '', ''),
			(50, 4, 1, 'NTE',  '2024-03-20T10:00:00Z', '', ''),
			(60, 1, 1, 'DEL',  '2024-04-01T10:00:00Z', '', '')`,

		`INSERT INTO deletedItems VALUES (60)`,

		`INSERT INTO itemDataValues VALUES
			(1,'Transformers in fMRI Analysis'),
			(2,'2024-03-15 March 15, 2024'),
			(3,'NeuroImage'),
			(4,'Citation Key: xyz'),
			(5,'Deep Space Report'),
			(6,'2023-00-00 2023'),
			(7,'Citation Key: abc'),
			(8,'A Book About Radium')`,

		`INSERT INTO itemData VALUES
			(10,1,1),(10,2,2),(10,3,3),(10,4,4),
			(20,1,5),(20,2,6),(20,4,7),
			(30,1,8)`,

		// itemCreators: (itemID, creatorID, creatorTypeID, orderIndex)
		// Item 10: Smith (author, order 0), Jones (author, order 1), Editor (editor, order 2 — must be filtered out)
		// Item 20: NASA (author)
		// Item 30: Curie (author)
		`INSERT INTO itemCreators VALUES
			(10,1,1,0),
			(10,2,1,1),
			(10,5,2,2),
			(20,3,1,0),
			(30,4,1,0)`,

		// Docling note child of item 10.
		`INSERT INTO items VALUES
			(90, 4, 1, 'NOTE90', '2024-03-16T10:00:00Z', '', '')`,
		`INSERT INTO itemNotes VALUES
			(90, 10, '<div class="zotero-note znv1"><pre>---
title: Extracted
---

# Heading

Some **bold** content.</pre></div>', 'Extraction Note')`,
		`INSERT INTO tags VALUES (1,'docling',0)`,
		`INSERT INTO itemTags VALUES (90,1,0)`,

		// PDF attachment for item 10 only. Items 20 and 30 have no PDF.
		`INSERT INTO itemAttachments VALUES (40, 10, 0, 'application/pdf')`,

		// Fulltext index tables (Zotero's manual word-level FTS).
		`CREATE TABLE fulltextWords (wordID INTEGER PRIMARY KEY, word TEXT UNIQUE)`,
		`CREATE TABLE fulltextItemWords (wordID INT, itemID INT, PRIMARY KEY (wordID, itemID))`,
		`CREATE TABLE fulltextItems (itemID INTEGER PRIMARY KEY, indexedPages INT, totalPages INT, indexedChars INT, totalChars INT)`,

		// Words linked to attachment 40 (parent item 10).
		`INSERT INTO fulltextWords VALUES (1,'transformer'),(2,'fmri'),(3,'analysis')`,
		`INSERT INTO fulltextItemWords VALUES (1,40),(2,40),(3,40)`,
		`INSERT INTO fulltextItems VALUES (40,10,10,NULL,NULL)`,
	}
	for _, s := range seed {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	return dir
}

func TestStoreNoteContent(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// Must call QueryTable first to populate the notes cache.
	_, _, _, _, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}

	// Item 10 has a docling note — NoteContent should return the unwrapped markdown.
	md := store.NoteContent(10)
	if md == "" {
		t.Fatal("NoteContent(10) = empty, want extracted markdown")
	}
	if !contains(md, "# Heading") {
		t.Errorf("NoteContent(10) should contain markdown heading, got %q", md)
	}
	if contains(md, "zotero-note") {
		t.Error("NoteContent should strip the Zotero wrapper div")
	}

	// Item 20 has no docling note.
	if md := store.NoteContent(20); md != "" {
		t.Errorf("NoteContent(20) = %q, want empty", md)
	}
}

// Compile-time assertion: Store must implement FulltextSearcher.
var _ data.FulltextSearcher = (*Store)(nil)

// Compile-time assertion: Store must implement SortKeyProvider so dbtui can
// sort the Date Added column chronologically rather than by display string.
var _ data.SortKeyProvider = (*Store)(nil)

// dateAddedCol is the column index of the "Date Added" column in items.
const dateAddedCol = 4

func TestStoreCellSortKeys_DateAdded(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// QueryTable must be called first to surface the rows whose keys we check.
	_, rows, _, _, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}

	keys, err := store.CellSortKeys(TableName)
	if err != nil {
		t.Fatalf("CellSortKeys: %v", err)
	}
	if len(keys) != len(rows) {
		t.Fatalf("keys len = %d, want %d", len(keys), len(rows))
	}

	// Raw Zotero dateAdded values are already ISO-ish UTC strings —
	// lexicographic sort over them matches chronological order.
	// Fixture: item 10 → 2024-03-15T10:00:00Z (row 0, most recent)
	//          item 20 → 2024-02-01T10:00:00Z (row 1)
	//          item 30 → 2024-01-01 10:00:00  (row 2, older encoding)
	want := []string{
		"2024-03-15T10:00:00Z",
		"2024-02-01T10:00:00Z",
		"2024-01-01 10:00:00",
	}
	for i, w := range want {
		if keys[i][dateAddedCol] != w {
			t.Errorf("row %d Date Added key = %q, want %q", i, keys[i][dateAddedCol], w)
		}
	}
}

func TestStoreCellSortKeys_WrongTable(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	if _, err := store.CellSortKeys("nonexistent"); err == nil {
		t.Error("expected error for unknown table")
	}
}

// TestStoreDateAddedSortChronological threads the Store's sort keys through
// tabstate.ApplySorts and asserts chronological ordering — the invariant the
// user-reported bug violated. Without SortKey, a lexicographic sort over
// "03/15/24, 10:00am" etc. would put Feb 2024 after March 2024 in descending
// order only by coincidence of month digit; the regression shows up as soon
// as rows straddle a year or month boundary.
func TestStoreDateAddedSortChronological(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	_, rows, nullFlags, rowIDs, err := store.QueryTable(TableName)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := store.CellSortKeys(TableName)
	if err != nil {
		t.Fatal(err)
	}

	// Minimal Tab build mimicking app/tab.go's construction.
	cols, _ := store.TableColumns(TableName)
	specs := make([]tabstate.ColumnSpec, len(cols))
	tcols := make([]table.Column, len(cols))
	for i, c := range cols {
		specs[i] = tabstate.ColumnSpec{Title: c.Name, DBName: c.Name, Kind: tabstate.CellText}
		tcols[i] = table.Column{Title: c.Name, Width: 10}
	}
	tbl := table.New(table.WithColumns(tcols))

	cellRows := make([][]tabstate.Cell, len(rows))
	tableRows := make([]table.Row, len(rows))
	meta := make([]tabstate.RowMeta, len(rows))
	for i, row := range rows {
		cells := make([]tabstate.Cell, len(row))
		tRow := make(table.Row, len(row))
		for j, v := range row {
			isNull := j < len(nullFlags[i]) && nullFlags[i][j]
			var k string
			if i < len(keys) && j < len(keys[i]) {
				k = keys[i][j]
			}
			cells[j] = tabstate.Cell{Value: v, Kind: tabstate.CellText, Null: isNull, SortKey: k}
			tRow[j] = v
		}
		cellRows[i] = cells
		tableRows[i] = tRow
		meta[i] = tabstate.RowMeta{ID: uint(i), RowID: rowIDs[i]}
	}
	tbl.SetRows(tableRows)

	tab := &tabstate.Tab{
		Name:         TableName,
		Table:        tbl,
		Rows:         meta,
		Specs:        specs,
		CellRows:     cellRows,
		Loaded:       true,
		FullRows:     tableRows,
		FullMeta:     meta,
		FullCellRows: cellRows,
		Sorts:        []tabstate.SortEntry{{Col: dateAddedCol, Dir: tabstate.SortDesc}},
	}
	tabstate.ApplySorts(tab)

	// Descending by Date Added: most recent (item 10) first, oldest last.
	wantRowIDs := []int64{10, 20, 30}
	for i, want := range wantRowIDs {
		if got := tab.Rows[i].RowID; got != want {
			t.Errorf("row %d: rowID = %d, want %d (descending chronological)", i, got, want)
		}
	}
}

func TestStoreSearchFulltext_Hit(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ids, err := store.SearchFulltext(TableName, []string{"transformer"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 10 {
		t.Errorf("got %v, want [10]", ids)
	}
}

func TestStoreSearchFulltext_Miss(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ids, err := store.SearchFulltext(TableName, []string{"nonexistent"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("got %v, want empty", ids)
	}
}

func TestStoreSearchFulltext_Prefix(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// "trans" should prefix-match "transformer".
	ids, err := store.SearchFulltext(TableName, []string{"trans"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 10 {
		t.Errorf("got %v, want [10]", ids)
	}
}

func TestStoreSearchFulltext_Exact(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// "trans" with exact=true should NOT match "transformer".
	ids, err := store.SearchFulltext(TableName, []string{"trans"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("exact 'trans' got %v, want empty", ids)
	}
}

func TestStoreSearchFulltext_WrongTable(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	_, err := store.SearchFulltext("nonexistent", []string{"x"}, false)
	if err == nil {
		t.Error("expected error for unknown table")
	}
}

func sliceEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
