package duck

// source.go — translate a file path (+ optional --table) into a duckdb
// SQL fragment that yields its rows. Each verb (Cols/Head/Tail/...) builds
// its query around the returned [Source].

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// Source is the SQL preamble + FROM-clause expression for a file.
//
// Preamble runs once before the verb's SELECT; non-empty only for sqlite/duckdb
// where we ATTACH the file under an alias. Expr is the FROM-clause fragment
// (e.g. read_csv_auto('p') or s."table").
type Source struct {
	Preamble string
	Expr     string
}

// Resolve dispatches on file extension and returns a [Source] for use in
// duckdb queries. table disambiguates sqlite/duckdb tables and xlsx sheets;
// it must be empty for single-table file types.
func Resolve(path, table string) (Source, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return Source{Expr: fmt.Sprintf("read_csv_auto('%s')", sqlEscape(path))}, nil
	case ".tsv":
		return Source{Expr: fmt.Sprintf("read_csv_auto('%s', delim='\t')", sqlEscape(path))}, nil
	case ".json":
		return Source{Expr: fmt.Sprintf("read_json_auto('%s')", sqlEscape(path))}, nil
	case ".jsonl", ".ndjson":
		return Source{Expr: fmt.Sprintf("read_json_auto('%s', format='newline_delimited')", sqlEscape(path))}, nil
	case ".parquet":
		return Source{Expr: fmt.Sprintf("'%s'", sqlEscape(path))}, nil
	case ".xlsx", ".xlsm":
		return resolveXLSX(path, table)
	case ".xls":
		return Source{}, fmt.Errorf("legacy .xls is not supported; convert to .xlsx (e.g. via libreoffice or excel)")
	case ".db", ".sqlite", ".sqlite3":
		return resolveSQLite(path, table)
	case ".duckdb":
		return resolveDuckDB(path, table)
	default:
		return Source{}, fmt.Errorf("unsupported file extension %q (supported: csv, tsv, json, jsonl, ndjson, parquet, xlsx, xlsm, db, sqlite, sqlite3, duckdb)", ext)
	}
}

func resolveXLSX(path, table string) (Source, error) {
	sheets, err := listSheets(path)
	if err != nil {
		return Source{}, err
	}
	if table == "" {
		switch len(sheets) {
		case 0:
			return Source{}, fmt.Errorf("xlsx contains no sheets")
		case 1:
			table = sheets[0]
		default:
			return Source{}, fmt.Errorf("file has %d sheets: %s — pass --table", len(sheets), strings.Join(sheets, ", "))
		}
	} else if !slices.Contains(sheets, table) {
		return Source{}, fmt.Errorf("sheet %q not found; available: %s", table, strings.Join(sheets, ", "))
	}
	return Source{Expr: fmt.Sprintf("read_xlsx('%s', sheet='%s')", sqlEscape(path), sqlEscape(table))}, nil
}

func resolveSQLite(path, table string) (Source, error) {
	// Note: no sqlite_all_varchar here. The SET is added by [promote]
	// after it inspects the declared schema via DESCRIBE (which only sees
	// declared types when sqlite_all_varchar is NOT set). promote then
	// wraps src.Expr in TRY_CAST projections that honor the declared
	// schema where data complies and fall back to VARCHAR otherwise.
	preamble := fmt.Sprintf("ATTACH '%s' AS s (TYPE SQLITE, READ_ONLY);", sqlEscape(path))
	// SQLite-specific listing query: query sqlite_master directly via
	// USE alias instead of SHOW TABLES FROM alias. duckdb's sqlite_scanner
	// translates view DDL during ATTACH and SHOW TABLES then re-parses
	// it; views that use single-letter table aliases (e.g. `FROM x s`)
	// trigger a Parser Error on the SQLite-style alias. sqlite_master
	// is the raw catalog and avoids translation entirely.
	listSQL := preamble + " USE s; SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
	return resolveAttached(path, table, preamble, "s", listSQL)
}

func resolveDuckDB(path, table string) (Source, error) {
	preamble := fmt.Sprintf("ATTACH '%s' AS d (READ_ONLY);", sqlEscape(path))
	listSQL := preamble + " SHOW TABLES FROM d"
	return resolveAttached(path, table, preamble, "d", listSQL)
}

// resolveAttached is the shared body for sqlite/duckdb dispatch: ATTACH
// the file under an alias, list tables to disambiguate, validate the
// chosen table, then build the Source. listSQL is the per-backend query
// that returns one row per table with column "name".
func resolveAttached(path, table, preamble, alias, listSQL string) (Source, error) {
	tables, err := listTablesByQuery(listSQL)
	if err != nil {
		return Source{}, fmt.Errorf("list tables in %s: %w", filepath.Base(path), err)
	}
	if table == "" {
		switch len(tables) {
		case 0:
			return Source{}, fmt.Errorf("%s contains no tables", filepath.Base(path))
		case 1:
			table = tables[0]
		default:
			return Source{}, fmt.Errorf("file has %d tables: %s — pass --table", len(tables), strings.Join(tables, ", "))
		}
	} else if !slices.Contains(tables, table) {
		return Source{}, fmt.Errorf("table %q not found; available: %s", table, strings.Join(tables, ", "))
	}
	if !store.IsSafeIdentifier(table) {
		return Source{}, fmt.Errorf("invalid table name %q (allowed: alphanumerics, underscore, space)", table)
	}
	return Source{
		Preamble: preamble,
		Expr:     fmt.Sprintf(`%s."%s"`, alias, table),
	}, nil
}

// listTablesByQuery executes a query that yields one column "name" per
// table and returns the parsed list.
func listTablesByQuery(sql string) ([]string, error) {
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse table listing: %w", err)
	}
	return lo.Map(rows, func(r struct {
		Name string `json:"name"`
	}, _ int) string {
		return r.Name
	}), nil
}

// sqlEscape doubles single quotes for safe interpolation into a duckdb
// single-quoted string literal. Use only inside '...'; not for identifiers.
func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
