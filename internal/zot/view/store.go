// Package view implements a read-only dbtui DataStore that surfaces a
// Zotero library as a single denormalized "items" table. It is consumed by
// the `zot view` command, which launches the dbtui viewer against this
// store so the user can browse their library in a spreadsheet-like UI
// without touching the live Zotero DB.
//
// Every write method returns ErrReadOnly — the store is a projection over
// internal/zot/local, which itself opens zotero.sqlite in
// mode=ro&immutable=1. The store also implements data.ViewLister and
// returns IsView(items)=true so dbtui forces the tab into read-only mode
// as a belt-and-suspenders against future key bindings.
package view

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/zot/local"
)

// TableName is the single virtual table the store exposes.
const TableName = "items"

// columnTitles defines both the column order and the display names shown
// by dbtui. Keep synchronised with QueryTable.
var columnTitles = []string{
	"Author(s)",
	"Year",
	"Journal/Publication",
	"Title",
	"Date Added",
	"Extra",
	"Notes",
	"PDF",
}

// noteIndicatorExtracted is the cell value shown when a docling note exists.
const noteIndicatorExtracted = "Extracted"

// noteIndicatorNone is the cell value shown when no docling note exists.
const noteIndicatorNone = "-"

// pdfIndicatorYes is the cell value shown when a PDF attachment exists.
const pdfIndicatorYes = "Yes"

// pdfIndicatorNone is the cell value shown when no PDF attachment exists.
const pdfIndicatorNone = "-"

// dateAddedLayouts covers both encodings seen in the wild on items.dateAdded:
// ISO-8601 with a T separator and trailing Z (current Zotero releases) and
// the older space-separated form. ParseInLocation assumes UTC for both —
// Zotero stores this field in UTC regardless of the user's timezone.
var dateAddedLayouts = []string{
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
}

// humanDateAddedFormat is the display format for the Date Added column —
// two-digit year, 12-hour clock, lowercase am/pm: e.g. "04/11/25, 4:31pm".
const humanDateAddedFormat = "01/02/06, 3:04pm"

// ErrReadOnly is returned by every mutating DataStore method.
var ErrReadOnly = errors.New("zot view: read-only store")

// Store implements data.DataStore and data.ViewLister over a local.DB.
// Store takes ownership of the passed local.DB — calling Close on the
// store closes the underlying connection.
type Store struct {
	db                local.Reader
	loc               *time.Location
	notesByRowID      map[int64]string // populated by QueryTable; rowID → unwrapped markdown
	lowerNotesByRowID map[int64]string // lower-cased note bodies for full-mode row search; built once per QueryTable

	// sortKeys is populated by QueryTable. sortKeys[i][j] is a
	// lexicographically-sortable key for the cell at (row i, col j), used by
	// dbtui when the display format isn't order-preserving. Only the
	// Date Added column is populated today; other columns get "".
	sortKeys [][]string
}

// New wraps a local.DB as a read-only DataStore. loc controls the timezone
// used to format the Date Added column; pass time.Local in production and
// time.UTC from tests for deterministic output.
func New(db local.Reader, loc *time.Location) *Store {
	if loc == nil {
		loc = time.Local
	}
	return &Store{db: db, loc: loc}
}

// ── DataStore: reads ────────────────────────────────────────────────────

// TableNames implements data.DataStore.
func (s *Store) TableNames() ([]string, error) {
	return []string{TableName}, nil
}

// TableColumns implements data.DataStore.
func (s *Store) TableColumns(table string) ([]data.PragmaColumn, error) {
	if table != TableName {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	cols := lo.Map(columnTitles, func(t string, i int) data.PragmaColumn {
		return data.PragmaColumn{CID: i, Name: t, Type: "TEXT"}
	})
	return cols, nil
}

// TableRowCount implements data.DataStore.
func (s *Store) TableRowCount(table string) (int, error) {
	if table != TableName {
		return 0, fmt.Errorf("unknown table %q", table)
	}
	return s.db.CountViewRows()
}

// QueryTable implements data.DataStore.
func (s *Store) QueryTable(table string) (
	colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error,
) {
	if table != TableName {
		return nil, nil, nil, nil, fmt.Errorf("unknown table %q", table)
	}
	raw, err := s.db.ListViewRows()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	noteBodies, err := s.db.DoclingNoteBodyByItemID()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Cache unwrapped markdown for NoteContent lookups. The lower-case copy
	// is retained for full-mode row search so we don't re-lowercase on every
	// keystroke; the unwrapped original stays intact for overlay previews.
	s.notesByRowID = make(map[int64]string, len(noteBodies))
	s.lowerNotesByRowID = make(map[int64]string, len(noteBodies))
	for id, body := range noteBodies {
		unwrapped := local.UnwrapZoteroDiv(body)
		s.notesByRowID[id] = unwrapped
		s.lowerNotesByRowID[id] = strings.ToLower(unwrapped)
	}

	colNames = make([]string, len(columnTitles))
	copy(colNames, columnTitles)

	rows = make([][]string, len(raw))
	nullFlags = make([][]bool, len(raw))
	rowIDs = make([]int64, len(raw))
	s.sortKeys = make([][]string, len(raw))
	for i, r := range raw {
		noteCell := noteIndicatorNone
		if _, ok := s.notesByRowID[r.ID]; ok {
			noteCell = noteIndicatorExtracted
		}
		pdfCell := pdfIndicatorNone
		if r.HasPDF {
			pdfCell = pdfIndicatorYes
		}
		rows[i] = []string{
			r.Authors,
			r.Year,
			r.Journal,
			r.Title,
			s.formatDateAdded(r.DateAdded),
			r.Extra,
			noteCell,
			pdfCell,
		}
		nullFlags[i] = make([]bool, len(columnTitles))
		rowIDs[i] = r.ID

		// Raw Zotero dateAdded is already an ISO-ish UTC string — both the
		// "2006-01-02T15:04:05Z" and "2006-01-02 15:04:05" forms are
		// lexicographically monotonic with time, so the raw value is a
		// valid sort key as-is. Other columns fall back to Value sorting.
		keys := make([]string, len(columnTitles))
		keys[4] = r.DateAdded
		s.sortKeys[i] = keys
	}
	return colNames, rows, nullFlags, rowIDs, nil
}

// CellSortKeys implements data.SortKeyProvider. Must be called after
// QueryTable, which populates the key matrix.
func (s *Store) CellSortKeys(table string) ([][]string, error) {
	if table != TableName {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	return s.sortKeys, nil
}

// ReadOnlyQuery implements data.DataStore.
func (s *Store) ReadOnlyQuery(query string) ([]string, [][]string, error) {
	return nil, nil, ErrReadOnly
}

// TableSummaries implements data.DataStore.
func (s *Store) TableSummaries() ([]data.TableSummary, error) {
	n, err := s.db.CountViewRows()
	if err != nil {
		return nil, err
	}
	return []data.TableSummary{{Name: TableName, Rows: n, Columns: len(columnTitles)}}, nil
}

// NoteContent returns the unwrapped markdown body for a docling note
// associated with the given rowID, or empty string if none exists.
// Must be called after QueryTable, which populates the notes cache.
func (s *Store) NoteContent(rowID int64) string {
	return s.notesByRowID[rowID]
}

// NoteBody implements data.NoteBodyProvider. Returns the pre-lowered note
// body for full-mode row search, or "" when no note exists. Guards the
// table name so unrelated dbtui tabs don't accidentally see notes.
func (s *Store) NoteBody(table string, rowID int64) string {
	if table != TableName {
		return ""
	}
	return s.lowerNotesByRowID[rowID]
}

// IsView marks the items table as a view so dbtui pins the tab read-only.
func (s *Store) IsView(name string) bool { return name == TableName }

// ── DataStore: writes (all blocked) ─────────────────────────────────────

// UpdateCell implements data.DataStore (always returns ErrReadOnly).
func (s *Store) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	return ErrReadOnly
}

// DeleteRows implements data.DataStore (always returns ErrReadOnly).
func (s *Store) DeleteRows(table string, ids []data.RowIdentifier) (int64, error) {
	return 0, ErrReadOnly
}

// InsertRows implements data.DataStore (always returns ErrReadOnly).
func (s *Store) InsertRows(table string, columns []string, rows [][]string) error {
	return ErrReadOnly
}

// RenameTable implements data.DataStore (always returns ErrReadOnly).
func (s *Store) RenameTable(oldName, newName string) error { return ErrReadOnly }

// DropTable implements data.DataStore (always returns ErrReadOnly).
func (s *Store) DropTable(table string) error { return ErrReadOnly }

// ExportCSV implements data.DataStore (always returns ErrReadOnly).
func (s *Store) ExportCSV(table, csvPath string) error { return ErrReadOnly }

// ImportCSV implements data.DataStore (always returns ErrImportNotSupported).
func (s *Store) ImportCSV(csvPath, tableName string) error {
	return data.ErrImportNotSupported
}

// ImportFile implements data.DataStore (always returns ErrImportNotSupported).
func (s *Store) ImportFile(filePath, tableName string) error {
	return data.ErrImportNotSupported
}

// CreateEmptyTable implements data.DataStore (always returns ErrReadOnly).
func (s *Store) CreateEmptyTable(tableName string) error { return ErrReadOnly }

// SearchFulltext implements data.FulltextSearcher by delegating to the
// underlying local.DB's fulltext word index.
func (s *Store) SearchFulltext(table string, words []string, exact bool) ([]int64, error) {
	if table != TableName {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	return s.db.SearchFulltext(words, exact)
}

// Close releases the underlying local.DB handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// formatDateAdded turns a "YYYY-MM-DD HH:MM:SS" UTC string into the human
// Date Added format localised to s.loc. Malformed inputs pass through as-is
// so we never eat underlying data.
func (s *Store) formatDateAdded(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range dateAddedLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t.In(s.loc).Format(humanDateAddedFormat)
		}
	}
	return raw
}
