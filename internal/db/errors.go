package db

// errors.go — the error vocabulary for `sci db`.
//
// Every failure a user can cause is classified before it reaches the CLI, so
// the --json envelope carries a code from cmdutil's closed vocabulary and the
// process exits on the right side of the partition: 2 when the command line
// has to be rewritten (usage), 1 when the work itself failed. Driver text
// ("SQL logic error: no such table: t (1)", "PRAGMA journal_mode = WAL: file
// is not a database (26)") never reaches the caller — it names SQLite
// internals the caller cannot act on.
//
// Fix carries a command that can be resubmitted verbatim; anything the user
// still has to think about goes in Try.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/store"
)

// fileKind is the on-disk shape of a path, read from its magic bytes rather
// than trusted from its extension.
type fileKind int

const (
	// kindUnknown is anything that is not a database — a CSV, a text file,
	// a Parquet file, a truncated download.
	kindUnknown fileKind = iota
	// kindEmpty is a zero-byte file, which SQLite treats as a valid empty
	// database and duckdb rejects.
	kindEmpty
	// kindSQLite starts with the 16-byte SQLite header.
	kindSQLite
	// kindDuckDB carries the "DUCK" magic at offset 8.
	kindDuckDB
)

// sqliteMagic is the header every SQLite database file starts with.
const sqliteMagic = "SQLite format 3\x00"

// sniffFileKind reads a file's leading bytes and reports what it actually is.
// A read failure returns kindUnknown — the caller is about to report a
// friendlier error about the same file anyway.
func sniffFileKind(path string) fileKind {
	f, err := os.Open(path)
	if err != nil {
		return kindUnknown
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, 16)
	n, err := io.ReadFull(f, header)
	if n == 0 && (err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
		return kindEmpty
	}
	header = header[:n]
	switch {
	case strings.HasPrefix(string(header), sqliteMagic):
		return kindSQLite
	case len(header) >= 12 && string(header[8:12]) == "DUCK":
		return kindDuckDB
	default:
		return kindUnknown
	}
}

// requireDatabase reports whether dbPath is a database the caller's verb can
// open, returning a coded error that says what to do when it is not. The
// extension picked the backend (see [isDuckDB]); this checks the bytes agree.
func requireDatabase(dbPath string) error {
	fi, err := os.Stat(dbPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return errDatabaseNotFound(dbPath)
	case err != nil:
		return cmdutil.Coded(cmdutil.CodeRuntime, "cannot read %s: %v", dbPath, err)
	case fi.IsDir():
		return cmdutil.Coded(cmdutil.CodeUsage, "%s is a directory, not a database", dbPath).
			WithTry("pass the database file itself")
	}

	kind := sniffFileKind(dbPath)
	wantDuck := isDuckDB(dbPath)
	switch {
	case kind == kindEmpty, wantDuck && kind == kindDuckDB, !wantDuck && kind == kindSQLite:
		return nil
	case wantDuck && kind == kindSQLite:
		return cmdutil.Coded(cmdutil.CodeUsage, "%s is a SQLite database with a .duckdb extension", dbPath).
			WithTry("rename it to *.db — sci picks the backend from the extension")
	case !wantDuck && kind == kindDuckDB:
		return cmdutil.Coded(cmdutil.CodeUsage, "%s is a DuckDB database with a SQLite extension", dbPath).
			WithTry("rename it to *.duckdb — sci picks the backend from the extension")
	default:
		return errNotADatabase(dbPath)
	}
}

// errDatabaseNotFound is the missing-database error shared by every verb.
func errDatabaseNotFound(dbPath string) error {
	return cmdutil.Coded(cmdutil.CodeNotFound, "database not found: %s", dbPath).
		WithTry(fmt.Sprintf("check the path, or create it with `sci db create %s`", dbPath))
}

// errNotADatabase covers a file that exists but is not a database at all —
// the caller almost always meant one of the flat-file verbs.
func errNotADatabase(path string) error {
	return cmdutil.Coded(cmdutil.CodeUsage, "%s is not a SQLite or DuckDB database", path).
		WithTry(fmt.Sprintf(
			"sci's database verbs read .db/.sqlite/.duckdb files; for tabular files (csv/tsv/json/jsonl/parquet/xlsx) try `sci db cols %s`",
			path))
}

// errTableNotFound names a table the database does not have.
func errTableNotFound(table, dbPath string) error {
	return cmdutil.Coded(cmdutil.CodeNotFound, "no table %q in %s", table, dbPath).
		WithTry(fmt.Sprintf("run `sci db info %s` to list the tables", dbPath))
}

// errTableExists is the rename-onto-a-taken-name refusal.
func errTableExists(table, dbPath string) error {
	return cmdutil.Coded(cmdutil.CodeUsage, "table %q already exists in %s", table, dbPath).
		WithTry(fmt.Sprintf("pick a free name, or drop the existing one with `sci db delete %s %s`", table, dbPath))
}

// requireInputFile checks a CSV (or other importable file) the caller named.
func requireInputFile(path string) error {
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cmdutil.Coded(cmdutil.CodeNotFound, "file not found: %s", path).
			WithTry("check the path — sci imports .csv, .tsv, .json, .jsonl")
	case err != nil:
		return cmdutil.Coded(cmdutil.CodeRuntime, "cannot read %s: %v", path, err)
	case fi.IsDir():
		return cmdutil.Coded(cmdutil.CodeUsage, "%s is a directory, not a file", path).
			WithTry(fmt.Sprintf("name the files to import, e.g. `%s/*.csv`", strings.TrimSuffix(path, string(filepath.Separator))))
	}
	return nil
}

// validateTableName checks that a user-supplied table name is safe for SQL.
func validateTableName(name string) error {
	if !store.IsSafeIdentifier(name) {
		return cmdutil.Coded(cmdutil.CodeUsage,
			"invalid table name: %q (only alphanumerics, underscores, and spaces allowed)", name)
	}
	return nil
}
