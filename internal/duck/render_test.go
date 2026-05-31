package duck

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestGlimpseHumanDplyrStyle(t *testing.T) {
	r := &GlimpseResult{
		RowCount: 42,
		Columns: []GlimpseColumn{
			{Name: "id", Type: "BIGINT", Samples: []any{json.Number("1"), json.Number("2")}},
			{Name: "title", Type: "VARCHAR", Samples: []any{"alice", nil}},
		},
	}
	out := stripANSI(r.Human())
	for _, want := range []string{"Rows: 42", "Columns: 2", "$ id", "<BIGINT>", "1, 2", `"alice"`, "NA"} {
		if !strings.Contains(out, want) {
			t.Errorf("glimpse Human() missing %q\n%s", want, out)
		}
	}
}

func TestRowsHumanPreservesColumnOrder(t *testing.T) {
	r := &RowsResult{
		Columns: []string{"zeta", "alpha"}, // not alphabetical — must be honored
		Rows: []map[string]any{
			{"zeta": json.Number("9"), "alpha": "x"},
		},
	}
	out := stripANSI(r.Human())
	zi, ai := strings.Index(out, "zeta"), strings.Index(out, "alpha")
	if zi == -1 || ai == -1 || zi > ai {
		t.Errorf("expected zeta column before alpha; got indices zeta=%d alpha=%d\n%s", zi, ai, out)
	}
}

func TestFormatCellExactNumbers(t *testing.T) {
	// json.Number preserves duckdb's exact text (no float64 rounding).
	if got := formatCell(json.Number("100000000000000001")); got != "100000000000000001" {
		t.Errorf("formatCell(big int) = %q, want exact text", got)
	}
	if got := formatCell(nil); got != "" {
		t.Errorf("formatCell(nil) = %q, want empty", got)
	}
	if got := formatCell(true); got != "true" {
		t.Errorf("formatCell(true) = %q", got)
	}
}

func TestColumnOrderFromJSON(t *testing.T) {
	// Nested STRUCT value must not derail key-order recovery.
	data := []byte(`[{"b":1,"nested":{"x":1,"y":2},"a":"hi"},{"b":2,"nested":{},"a":"yo"}]`)
	cols, err := columnOrder(data)
	if err != nil {
		t.Fatalf("columnOrder: %v", err)
	}
	want := []string{"b", "nested", "a"}
	if strings.Join(cols, ",") != strings.Join(want, ",") {
		t.Errorf("columnOrder = %v, want %v", cols, want)
	}
}
