package duck

import (
	"os"
	"strings"
	"testing"
)

// TestResolveStateless covers the source-dispatch branches that need no
// duckdb binary: csv/tsv/json/jsonl/parquet (pure SQL string assembly) and
// xlsx (sheet listing via Go zip parser).
func TestResolveStateless(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		table       string
		wantPreEmpt bool   // true if Source.Preamble should be empty
		wantExpr    string // exact match for Source.Expr
		wantErr     string // substring of error message; "" means expect success
	}{
		{
			name:        "csv",
			path:        "/tmp/foo.csv",
			wantPreEmpt: true,
			wantExpr:    "read_csv_auto('/tmp/foo.csv')",
		},
		{
			name:        "csv uppercase ext",
			path:        "/tmp/FOO.CSV",
			wantPreEmpt: true,
			wantExpr:    "read_csv_auto('/tmp/FOO.CSV')",
		},
		{
			name:        "tsv passes delim",
			path:        "/tmp/foo.tsv",
			wantPreEmpt: true,
			wantExpr:    "read_csv_auto('/tmp/foo.tsv', delim='\t')",
		},
		{
			name:        "json",
			path:        "/tmp/foo.json",
			wantPreEmpt: true,
			wantExpr:    "read_json_auto('/tmp/foo.json')",
		},
		{
			name:        "jsonl uses newline_delimited",
			path:        "/tmp/foo.jsonl",
			wantPreEmpt: true,
			wantExpr:    "read_json_auto('/tmp/foo.jsonl', format='newline_delimited')",
		},
		{
			name:        "ndjson uses newline_delimited",
			path:        "/tmp/foo.ndjson",
			wantPreEmpt: true,
			wantExpr:    "read_json_auto('/tmp/foo.ndjson', format='newline_delimited')",
		},
		{
			name:        "parquet uses bare quoted literal",
			path:        "/tmp/foo.parquet",
			wantPreEmpt: true,
			wantExpr:    "'/tmp/foo.parquet'",
		},
		{
			name:        "xlsx single sheet auto-picked",
			path:        "testdata/single_sheet.xlsx",
			wantPreEmpt: true,
			wantExpr:    "read_xlsx('testdata/single_sheet.xlsx', sheet='only')",
		},
		{
			name:        "xlsx multi-sheet with --table",
			path:        "testdata/tiny.xlsx",
			table:       "extras",
			wantPreEmpt: true,
			wantExpr:    "read_xlsx('testdata/tiny.xlsx', sheet='extras')",
		},
		{
			name:    "xlsx multi-sheet without --table errors with sheet list",
			path:    "testdata/tiny.xlsx",
			wantErr: "people, extras",
		},
		{
			name:    "xlsx unknown sheet errors with available list",
			path:    "testdata/tiny.xlsx",
			table:   "missing",
			wantErr: "available",
		},
		{
			name:    "xls is rejected with conversion hint",
			path:    "/tmp/legacy.xls",
			wantErr: ".xlsx",
		},
		{
			name:    "unknown extension is rejected",
			path:    "/tmp/data.unknown",
			wantErr: "unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := Resolve(tt.path, tt.table)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (src=%+v)", tt.wantErr, src)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if tt.wantPreEmpt && src.Preamble != "" {
				t.Errorf("Preamble = %q, want empty", src.Preamble)
			}
			if src.Expr != tt.wantExpr {
				t.Errorf("Expr = %q, want %q", src.Expr, tt.wantExpr)
			}
		})
	}
}

// TestResolveXLSXEscapesQuotes confirms single quotes in sheet names or paths
// are escaped (xlsx allows quotes in sheet names; SQL-injection risk).
func TestResolveXLSXEscapesQuotes(t *testing.T) {
	// We don't have a quoted-name fixture; just unit-test the escaper directly.
	if got := sqlEscape("a'b"); got != "a''b" {
		t.Errorf("sqlEscape(\"a'b\") = %q, want %q", got, "a''b")
	}
	if got := sqlEscape("plain"); got != "plain" {
		t.Errorf("sqlEscape(\"plain\") = %q, want %q", got, "plain")
	}
}

func TestQuoteIdent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"users", `"users"`},
		{"my table", `"my table"`},
		{`weird"name`, `"weird""name"`},
		{"", `""`},
	}
	for _, tt := range tests {
		if got := quoteIdent(tt.in); got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestResolveSQLite covers the sqlite branch which needs the duckdb binary
// to enumerate tables via ATTACH + SHOW TABLES.
func TestResolveSQLite(t *testing.T) {
	requireDuck(t)

	t.Run("single-table auto-pick", func(t *testing.T) {
		src, err := Resolve(singleDB, "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(src.Preamble, "TYPE SQLITE") || !strings.Contains(src.Preamble, "READ_ONLY") {
			t.Errorf("Preamble = %q, want it to ATTACH ... TYPE SQLITE READ_ONLY", src.Preamble)
		}
		if !strings.Contains(src.Expr, `"only_one"`) {
			t.Errorf("Expr = %q, want it to reference \"only_one\"", src.Expr)
		}
	})

	t.Run("multi-table no flag errors with table list", func(t *testing.T) {
		_, err := Resolve(tinyDB, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "people") || !strings.Contains(msg, "extras") {
			t.Errorf("error %q does not list both tables", msg)
		}
	})

	t.Run("with valid --table", func(t *testing.T) {
		src, err := Resolve(tinyDB, "extras")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(src.Expr, `"extras"`) {
			t.Errorf("Expr = %q, want it to reference \"extras\"", src.Expr)
		}
	})

	t.Run("unknown --table errors", func(t *testing.T) {
		_, err := Resolve(tinyDB, "missing")
		if err == nil || !strings.Contains(err.Error(), "available") {
			t.Errorf("got %v, want an error mentioning available tables", err)
		}
	})

	t.Run("unsafe --table identifier rejected", func(t *testing.T) {
		_, err := Resolve(tinyDB, "people; DROP TABLE x")
		if err == nil {
			t.Error("expected unsafe-identifier error, got nil")
		}
	})

	// Regression: duckdb's sqlite_scanner translates SQLite view definitions
	// during ATTACH. SHOW TABLES FROM <alias> then errors on views that use
	// single-letter table aliases (e.g. `FROM similar s`). We must list
	// tables via sqlite_master instead. Triggered by zot-graph.db in the
	// wild; reproduced here with viewy.db.
	t.Run("ATTACH-with-view-using-single-letter-alias still lists tables", func(t *testing.T) {
		if _, err := os.Stat(viewyDB); err != nil {
			t.Skipf("viewy.db fixture not generated (sqlite3 missing?): %v", err)
		}
		src, err := Resolve(viewyDB, "similar")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(src.Expr, `"similar"`) {
			t.Errorf("Expr = %q, want it to reference \"similar\"", src.Expr)
		}
	})
}

// TestResolveDuckDB exercises the duckdb-file branch (.duckdb extension).
func TestResolveDuckDB(t *testing.T) {
	requireDuck(t)

	t.Run("single-table auto-pick", func(t *testing.T) {
		src, err := Resolve(singleDuck, "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(src.Preamble, "READ_ONLY") || strings.Contains(src.Preamble, "TYPE SQLITE") {
			t.Errorf("Preamble = %q, want it to ATTACH READ_ONLY without TYPE SQLITE", src.Preamble)
		}
		if !strings.Contains(src.Expr, `"only_one"`) {
			t.Errorf("Expr = %q, want it to reference \"only_one\"", src.Expr)
		}
	})

	t.Run("multi-table no flag errors with table list", func(t *testing.T) {
		_, err := Resolve(tinyDuck, "")
		if err == nil || !strings.Contains(err.Error(), "people") {
			t.Errorf("got %v, want error listing tables", err)
		}
	})
}
