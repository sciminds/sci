package store

import "fmt"

// MaxTableRows is the maximum number of rows loaded from a single table into the TUI.
const MaxTableRows = 50_000

// RowIdentifier addresses a row by its SQLite rowid or primary key values.
type RowIdentifier struct {
	RowID    int64
	PKValues map[string]string
}

// PragmaColumn mirrors the output of SQLite's PRAGMA table_info. The same
// shape is used by the DuckDB backend (populated from information_schema).
type PragmaColumn struct {
	CID       int
	Name      string
	Type      string
	NotNull   bool
	DfltValue *string
	PK        int
}

// TableSummary holds lightweight metadata for the table list overlay.
type TableSummary struct {
	Name    string
	Rows    int
	Columns int
}

// DataStore abstracts database access for both SQLite and DuckDB backends.
type DataStore interface { //nolint:revive // name is established in the API
	// TableNames returns all user table names, alphabetically.
	TableNames() ([]string, error)

	// TableColumns returns column metadata for the named table.
	TableColumns(table string) ([]PragmaColumn, error)

	// TableRowCount returns the number of rows in the named table.
	TableRowCount(table string) (int, error)

	// QueryTable returns all rows from the named table as string slices.
	QueryTable(table string) (colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error)

	// ReadOnlyQuery executes a validated SELECT query.
	ReadOnlyQuery(query string) (columns []string, rows [][]string, err error)

	// UpdateCell updates a single cell identified by rowID (and optionally pkValues
	// for stores that use explicit primary keys instead of rowid).
	UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error

	// DeleteRows removes rows identified by RowIdentifier and returns the
	// number of rows actually deleted.
	DeleteRows(table string, ids []RowIdentifier) (int64, error)

	// InsertRows inserts rows into the named table. columns lists the
	// column names; each entry in rows is one row of string values.
	// Empty strings are inserted as NULL.
	InsertRows(table string, columns []string, rows [][]string) error

	// TableSummaries returns table names with row counts and column counts
	// in a single efficient query. Backends should batch this when possible.
	TableSummaries() ([]TableSummary, error)

	// RenameTable renames a table from oldName to newName.
	RenameTable(oldName, newName string) error

	// DropTable drops the named table from the database.
	DropTable(table string) error

	// ExportCSV exports the named table to a CSV file at the given path.
	ExportCSV(table, csvPath string) error

	// ImportCSV imports a CSV file as a new table.
	ImportCSV(csvPath, tableName string) error

	// AppendCSV appends rows from a CSV file into an existing table.
	// Returns an error if the table does not exist.
	AppendCSV(csvPath, tableName string) error

	// ImportFile imports a file as a new table, auto-detecting format by extension.
	// Supported: .csv, .tsv, .json, .jsonl, .ndjson.
	// Returns an error for unsupported formats.
	ImportFile(filePath, tableName string) error

	// CreateEmptyTable creates a new empty table with a default schema
	// (id INTEGER PRIMARY KEY, name TEXT, value TEXT).
	CreateEmptyTable(tableName string) error

	// Close closes the database connection.
	Close() error
}

// ViewLister is an optional interface that DataStore implementations may
// provide to indicate which names returned by TableNames are SQL views.
// The viewer uses this to mark view tabs as read-only.
type ViewLister interface {
	IsView(name string) bool
}

// VirtualLister is an optional interface that DataStore implementations may
// provide to indicate which names returned by TableNames are virtual tables
// (e.g. FTS5 shadow tables, WITHOUT ROWID tables).
type VirtualLister interface {
	IsVirtual(name string) bool
}

// NoteBodyProvider is an optional interface that DataStore implementations
// may provide to supply pre-lowercased note bodies for full-mode row search.
// When present, unscoped queries in modeFull scan these bodies for
// token-AND substring matches, tagging hits with originNote so the Notes
// indicator cell gets origin tinting. Returning "" means "no note for this
// row" — callers skip it. Bodies are expected to be pre-lowered so the hot
// path is O(tokens × body-length) without per-keystroke re-lowercasing.
type NoteBodyProvider interface {
	NoteBody(table string, rowID int64) string
}

// NoteContentProvider is an optional interface that DataStore implementations
// may provide to supply rich markdown content for cell preview overlays.
// When a cell is selected and the store implements this interface, the TUI
// checks NoteContent(rowID) — if non-empty, the preview uses a markdown
// overlay instead of plain text.
type NoteContentProvider interface {
	NoteContent(rowID int64) string
}

// SortKeyProvider is an optional interface that DataStore implementations
// may provide to supply per-cell sort keys for columns whose display format
// is not lexicographically comparable (e.g. human-formatted dates like
// "04/11/25, 4:31pm"). The returned matrix is parallel to the rows returned
// by QueryTable — CellSortKeys[i][j] is the sort key for column j of row i.
// Empty strings mean "sort by Value". Only consulted once during tab build.
type SortKeyProvider interface {
	CellSortKeys(table string) ([][]string, error)
}

// FulltextSearcher is an optional interface that DataStore implementations may
// provide to support content-level fulltext search (e.g. PDF body text).
// When implemented, unscoped search queries union fulltext hits with fuzzy
// column matches, widening recall to include items whose content matches even
// when their visible metadata does not.
type FulltextSearcher interface {
	// SearchFulltext returns rowIDs matching all given words in the fulltext
	// index. Words are prefix-matched by default; exact when exact is true.
	SearchFulltext(table string, words []string, exact bool) ([]int64, error)
}

// ErrImportNotSupported is returned by backends that do not support import.
var ErrImportNotSupported = fmt.Errorf("import is not supported for this database type")
