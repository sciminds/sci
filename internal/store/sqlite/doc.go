// Package sqlite implements [store.DataStore] over the pure-Go
// modernc.org/sqlite driver (no CGO required).
//
// # Stores
//
//   - [Open] returns a [Store] connected to a file-backed SQLite database
//     with WAL mode, foreign keys, busy-timeout, and a 256 MB mmap window.
//   - [OpenMemory] returns a [Store] backed by an in-memory shared-cache
//     database — useful for tests and for the [FileView] flat-file viewer.
//   - [OpenFileView] returns a [FileView] that imports a CSV, TSV, JSON,
//     or JSONL file into an in-memory SQLite table and writes back to the
//     original file on Close (when mutated).
//
// Store supports full CRUD via SQLite's implicit rowid, schema mutations
// (rename/drop columns and tables), CSV export, and type-inferred import
// from CSV, TSV, JSON, and JSONL.
package sqlite
