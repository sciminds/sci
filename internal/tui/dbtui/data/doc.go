// Package data provides the database backend for dbtui.
//
// It defines the [DataStore] interface that abstracts all database access,
// and provides [Store], a pure-Go SQLite implementation backed by
// modernc.org/sqlite (no CGO required).
//
// # Store
//
// [Open] returns a [Store] connected to a SQLite file with WAL mode enabled.
// Store supports full CRUD operations addressed by SQLite's implicit rowid,
// as well as schema mutations (rename/drop columns and tables), CSV export,
// and type-inferred import from CSV, TSV, JSON, and JSONL files.
//
// # SQL Safety
//
// All table and column names are validated through [IsSafeIdentifier] before
// interpolation into SQL. Ad-hoc queries are restricted to read-only SELECTs
// via [ValidateReadOnlySQL], which rejects multi-statement strings and
// write keywords in CTEs.
//
// # Import
//
// [Store.ImportFile] auto-detects format by file extension and infers column
// types (INTEGER, REAL, or TEXT) by sampling all values. Supported extensions
// are listed by [ImportableExtensions]. [TableNameFromFile] derives a SQL-safe
// table name from a file path.
package data
