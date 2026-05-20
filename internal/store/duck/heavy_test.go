package duck

import (
	"strings"
	"testing"
)

func TestIsHeavyType(t *testing.T) {
	tests := []struct {
		typ  string
		want bool
	}{
		{"VARCHAR", false},
		{"INTEGER", false},
		{"BIGINT", false},
		{"DOUBLE", false},
		{"BOOLEAN", false},
		{"TIMESTAMP", false},
		{"INTERVAL", false},
		{"UUID", false},
		{"", false},

		{"BLOB", true},
		{"blob", true},
		{"BIT", true},
		{"JSON", true},

		{"FLOAT[]", true},
		{"FLOAT[768]", true},
		{"INTEGER[10]", true},
		{"VARCHAR[]", true},
		{"STRUCT(x INTEGER)[]", true},

		{"STRUCT(x INTEGER, y DOUBLE)", true},
		{"MAP(VARCHAR, INTEGER)", true},
		{"UNION(num INTEGER, txt VARCHAR)", true},

		// Edge: type with weird whitespace but valid shape.
		{"  STRUCT(x INTEGER)  ", true},
	}
	for _, tc := range tests {
		if got := isHeavyType(tc.typ); got != tc.want {
			t.Errorf("isHeavyType(%q) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}

// TestHeavyPlaceholderExprShape spot-checks the SQL fragments emitted for
// each branch. We don't execute them here — that's covered by the
// integration test in store_test.go — we just verify the construction is
// stable (the col name is double-quoted, the type name appears as a
// literal, NULL handling is wired in).
func TestHeavyPlaceholderExprShape(t *testing.T) {
	tests := []struct {
		col, typ string
		wantSubs []string
	}{
		{"embedding", "FLOAT[]", []string{`"embedding"`, "'<FLOAT'", "LEN(\"embedding\")", "IS NULL THEN NULL"}},
		{"embedding", "FLOAT[768]", []string{`"embedding"`, "'<FLOAT'", "LEN(\"embedding\")"}},
		{"vec", "BLOB", []string{`"vec"`, "OCTET_LENGTH(\"vec\")", "'<BLOB '", " bytes>'"}},
		{"meta", "JSON", []string{`"meta"`, "CAST(\"meta\" AS VARCHAR)", "'<JSON '", " chars>'"}},
		{"counts", "MAP(VARCHAR, INTEGER)", []string{"LEN(\"counts\")", "'<MAP['"}},
		{"info", "STRUCT(x INTEGER)", []string{`"info"`, "'<STRUCT>'"}},
		{"u", "UNION(a INTEGER, b VARCHAR)", []string{`"u"`, "'<UNION>'"}},
	}
	for _, tc := range tests {
		got, err := heavyPlaceholderExpr(tc.col, tc.typ)
		if err != nil {
			t.Errorf("heavyPlaceholderExpr(%q,%q) err = %v", tc.col, tc.typ, err)
			continue
		}
		for _, sub := range tc.wantSubs {
			if !strings.Contains(got, sub) {
				t.Errorf("heavyPlaceholderExpr(%q,%q):\n  got: %s\n  missing substring: %q",
					tc.col, tc.typ, got, sub)
			}
		}
	}
}

func TestHeavyPlaceholderExprRejectsUnsafeColumn(t *testing.T) {
	if _, err := heavyPlaceholderExpr(`weird"; DROP TABLE x; --`, "FLOAT[]"); err == nil {
		t.Fatalf("expected error for unsafe column name")
	}
}

func TestArrayBaseLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"FLOAT[]", "FLOAT"},
		{"FLOAT[768]", "FLOAT"},
		{"INTEGER[10]", "INTEGER"},
		{"VARCHAR[]", "VARCHAR"},
		{"STRUCT(x INTEGER)[]", "STRUCT(x INTEGER)"},
		{"FLOAT[][]", "FLOAT[]"}, // strip outermost only
	}
	for _, tc := range tests {
		if got := arrayBaseLabel(tc.in); got != tc.want {
			t.Errorf("arrayBaseLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
