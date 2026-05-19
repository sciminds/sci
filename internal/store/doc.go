// Package store defines the DataStore interface that abstracts all
// database access for sci's TUI and CLI verbs, plus the SQLite and
// (later) DuckDB implementations under [store/sqlite] and [store/duck].
//
// # DataStore
//
// [DataStore] is implemented by both backends. SQLite uses
// modernc.org/sqlite (pure Go, no CGO); DuckDB shells out to the duckdb
// CLI via [internal/duck]. Callers pick an implementation by file
// extension via [Open] or by direct constructor (e.g.
// [sqlite.Open], [sqlite.OpenFileView]).
//
// # SQL safety
//
// All table and column names are validated through [IsSafeIdentifier]
// or [IsSafeColumnName] before interpolation. Ad-hoc queries are
// restricted to read-only SELECTs via [ValidateReadOnlySQL], which
// rejects multi-statement strings and write keywords in CTEs.
//
// # Import
//
// [TableNameFromFile] derives a SQL-safe table name from a file path.
// The SQLite backend's [sqlite.Store.ImportFile] auto-detects format by
// extension and infers column types (INTEGER, REAL, TEXT) by sampling.
package store
