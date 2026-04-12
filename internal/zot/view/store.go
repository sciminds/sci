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
	"time"

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
}

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
	db  local.Reader
	loc *time.Location
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

func (s *Store) TableNames() ([]string, error) {
	return []string{TableName}, nil
}

func (s *Store) TableColumns(table string) ([]data.PragmaColumn, error) {
	if table != TableName {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	cols := make([]data.PragmaColumn, len(columnTitles))
	for i, t := range columnTitles {
		cols[i] = data.PragmaColumn{CID: i, Name: t, Type: "TEXT"}
	}
	return cols, nil
}

func (s *Store) TableRowCount(table string) (int, error) {
	if table != TableName {
		return 0, fmt.Errorf("unknown table %q", table)
	}
	return s.db.CountViewRows()
}

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

	colNames = make([]string, len(columnTitles))
	copy(colNames, columnTitles)

	rows = make([][]string, len(raw))
	nullFlags = make([][]bool, len(raw))
	rowIDs = make([]int64, len(raw))
	for i, r := range raw {
		rows[i] = []string{
			r.Authors,
			r.Year,
			r.Journal,
			r.Title,
			s.formatDateAdded(r.DateAdded),
			r.Extra,
		}
		nullFlags[i] = make([]bool, len(columnTitles))
		rowIDs[i] = r.ID
	}
	return colNames, rows, nullFlags, rowIDs, nil
}

func (s *Store) ReadOnlyQuery(query string) ([]string, [][]string, error) {
	return nil, nil, ErrReadOnly
}

func (s *Store) TableSummaries() ([]data.TableSummary, error) {
	n, err := s.db.CountViewRows()
	if err != nil {
		return nil, err
	}
	return []data.TableSummary{{Name: TableName, Rows: n, Columns: len(columnTitles)}}, nil
}

// IsView marks the items table as a view so dbtui pins the tab read-only.
func (s *Store) IsView(name string) bool { return name == TableName }

// ── DataStore: writes (all blocked) ─────────────────────────────────────

func (s *Store) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	return ErrReadOnly
}

func (s *Store) DeleteRows(table string, ids []data.RowIdentifier) (int64, error) {
	return 0, ErrReadOnly
}

func (s *Store) InsertRows(table string, columns []string, rows [][]string) error {
	return ErrReadOnly
}

func (s *Store) RenameTable(oldName, newName string) error { return ErrReadOnly }
func (s *Store) DropTable(table string) error              { return ErrReadOnly }
func (s *Store) ExportCSV(table, csvPath string) error     { return ErrReadOnly }
func (s *Store) ImportCSV(csvPath, tableName string) error {
	return data.ErrImportNotSupported
}
func (s *Store) ImportFile(filePath, tableName string) error {
	return data.ErrImportNotSupported
}
func (s *Store) CreateEmptyTable(tableName string) error { return ErrReadOnly }

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
