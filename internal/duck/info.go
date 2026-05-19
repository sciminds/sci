package duck

// info.go — whole-database metadata for a duckdb file: list every
// table/view with its row count and column count. Used by `sci db info`
// to render the same summary it shows for SQLite databases.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// TableMeta is one entry in the Info() result: a base table or view in
// the duckdb file along with exact row/column counts.
type TableMeta struct {
	Name    string
	Rows    int
	Columns int
	IsView  bool
}

// Info enumerates the tables and views in a duckdb file and reports row
// + column counts for each. Returns ErrNotInstalled (wrapped) when the
// duckdb CLI is missing. Tables are returned in name-sorted order.
func Info(path string) ([]TableMeta, error) {
	preamble := fmt.Sprintf("ATTACH '%s' AS d (READ_ONLY);", sqlEscape(path))

	// information_schema.tables lists both BASE TABLE and VIEW for the
	// attached catalog. Filtering by table_catalog='d' scopes us to the
	// ATTACHed file (excludes the implicit in-memory catalog).
	listSQL := preamble +
		" SELECT table_name AS name, table_type AS type" +
		" FROM information_schema.tables" +
		" WHERE table_catalog='d' ORDER BY name"
	listOut, err := runJSON(listSQL)
	if err != nil {
		return nil, fmt.Errorf("list tables in %s: %w", filepath.Base(path), err)
	}
	var listed []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(listOut, &listed); err != nil {
		return nil, fmt.Errorf("parse table listing: %w", err)
	}
	if len(listed) == 0 {
		return nil, nil
	}

	// All table names come from the catalog, but interpolation is still
	// done via double-quoted identifiers — defensively validate so a
	// pathological name can't break the SQL.
	for _, r := range listed {
		if !store.IsSafeIdentifier(r.Name) {
			return nil, fmt.Errorf("unsupported table name %q in %s", r.Name, filepath.Base(path))
		}
	}

	// Column counts: one query gives us every table's column count.
	colSQL := preamble +
		" SELECT table_name AS name, COUNT(*) AS n" +
		" FROM information_schema.columns" +
		" WHERE table_catalog='d' GROUP BY table_name"
	colOut, err := runJSON(colSQL)
	if err != nil {
		return nil, fmt.Errorf("column counts in %s: %w", filepath.Base(path), err)
	}
	var colRows []struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	if err := json.Unmarshal(colOut, &colRows); err != nil {
		return nil, fmt.Errorf("parse column counts: %w", err)
	}
	colsByName := lo.SliceToMap(colRows, func(r struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}) (string, int) {
		return r.Name, r.N
	})

	// Row counts: UNION ALL one COUNT(*) per table in a single query.
	parts := lo.Map(listed, func(r struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}, _ int) string {
		return fmt.Sprintf(`SELECT '%s' AS name, COUNT(*) AS n FROM d.%s`,
			sqlEscape(r.Name), quoteIdent(r.Name))
	})
	rowSQL := preamble + " " + strings.Join(parts, " UNION ALL ")
	rowOut, err := runJSON(rowSQL)
	if err != nil {
		return nil, fmt.Errorf("row counts in %s: %w", filepath.Base(path), err)
	}
	var rowRows []struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	if err := json.Unmarshal(rowOut, &rowRows); err != nil {
		return nil, fmt.Errorf("parse row counts: %w", err)
	}
	rowsByName := lo.SliceToMap(rowRows, func(r struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}) (string, int) {
		return r.Name, r.N
	})

	return lo.Map(listed, func(r struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}, _ int) TableMeta {
		return TableMeta{
			Name:    r.Name,
			Rows:    rowsByName[r.Name],
			Columns: colsByName[r.Name],
			IsView:  strings.EqualFold(r.Type, "VIEW"),
		}
	}), nil
}
