package duck

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/lo"
)

// TestLossyColumnsMatrix seeds a duckdb with one column of each
// representative type and verifies that LossyColumns flags exactly the
// rich types — STRUCT, MAP, INTERVAL, arrays/LIST, UNION — while
// leaving DATE/DECIMAL/UUID/etc. alone.
func TestLossyColumnsMatrix(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rich.duckdb")
	// Note: union types are not exercised here; older duckdb builds
	// reject the syntax. The matcher still covers UNION via prefix.
	seed := fmt.Sprintf(`ATTACH '%s' AS d;
		CREATE TABLE d.demo (
			id INTEGER,
			name VARCHAR,
			born DATE,
			fp DECIMAL(10,2),
			tags VARCHAR[],
			profile STRUCT(birth INTEGER, city VARCHAR),
			span INTERVAL,
			props MAP(VARCHAR, INTEGER)
		);
		DETACH d;`, path)
	if _, err := runJSON(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := LossyColumns(path)
	if err != nil {
		t.Fatalf("LossyColumns: %v", err)
	}
	wantCols := []string{"tags", "profile", "span", "props"}
	gotByCol := lo.SliceToMap(got, func(c LossyColumn) (string, LossyColumn) {
		return c.Column, c
	})
	for _, name := range wantCols {
		if _, ok := gotByCol[name]; !ok {
			t.Errorf("expected lossy column %q in result, got %+v", name, got)
		}
	}
	for _, c := range got {
		if c.Table != "demo" {
			t.Errorf("unexpected table %q in result", c.Table)
		}
	}
	if len(got) != len(wantCols) {
		t.Errorf("got %d lossy columns, want %d: %+v", len(got), len(wantCols), got)
	}
}

// TestLossyColumnsCleanFile returns empty when no rich types present.
func TestLossyColumnsCleanFile(t *testing.T) {
	requireDuck(t)
	got, err := LossyColumns(tinyDuck)
	if err != nil {
		t.Fatalf("LossyColumns: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("clean duckdb produced lossy result: %+v", got)
	}
}

// TestIsLossyType is a pure-function unit test for the matcher so we
// can edge-case it without spinning up duckdb.
func TestIsLossyType(t *testing.T) {
	cases := map[string]bool{
		"INTEGER":                         false,
		"BIGINT":                          false,
		"VARCHAR":                         false,
		"DOUBLE":                          false,
		"DATE":                            false,
		"TIMESTAMP":                       false,
		"DECIMAL(10,2)":                   false,
		"UUID":                            false,
		"VARCHAR[]":                       true,
		"INTEGER[5]":                      true,
		"STRUCT(a INTEGER, b VARCHAR)":    true,
		"MAP(VARCHAR, INTEGER)":           true,
		"INTERVAL":                        true,
		"UNION(num INTEGER, txt VARCHAR)": true,
		"LIST(INTEGER)":                   true,
	}
	for typ, want := range cases {
		if got := isLossyType(typ); got != want {
			t.Errorf("isLossyType(%q) = %v, want %v", typ, got, want)
		}
	}
	// Case insensitivity.
	if !isLossyType(strings.ToLower("STRUCT(a INTEGER)")) {
		t.Error("isLossyType should be case-insensitive on STRUCT")
	}
}
