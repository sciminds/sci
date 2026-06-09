package duck

import (
	"reflect"
	"testing"
)

// TestSanitizeSpecialFloats checks that DuckDB's bare NaN/Infinity/-Infinity
// tokens become quoted text, that occurrences inside string values are left
// alone, and that input without them is returned unchanged.
func TestSanitizeSpecialFloats(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"nan", `{"x":NaN}`, `{"x":"NaN"}`},
		{"posinf", `{"x":Infinity}`, `{"x":"Inf"}`},
		{"neginf", `{"x":-Infinity}`, `{"x":"-Inf"}`},
		{"mixed", `{"a":NaN,"b":Infinity,"c":-Infinity,"d":null}`, `{"a":"NaN","b":"Inf","c":"-Inf","d":null}`},
		{"negative number untouched", `{"x":-1.5}`, `{"x":-1.5}`},
		{"token inside string untouched", `{"name":"NaN of Infinity"}`, `{"name":"NaN of Infinity"}`},
		{"no special floats", `{"g":"a","x":1.5}`, `{"g":"a","x":1.5}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(SanitizeSpecialFloats([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("SanitizeSpecialFloats(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeSpecialFloatsLeavesInputIntact confirms the function does not
// mutate its argument (callers pass the same buffer to decodeRows and
// columnOrder).
func TestSanitizeSpecialFloatsLeavesInputIntact(t *testing.T) {
	in := []byte(`{"x":NaN}`)
	orig := string(in)
	_ = SanitizeSpecialFloats(in)
	if string(in) != orig {
		t.Errorf("input mutated: %s", in)
	}
}

// TestDecodeRowsHandlesSpecialFloats confirms the array decoder no longer
// chokes on NaN/Infinity rows (the bug: "invalid character 'N'") and renders
// them as text distinct from JSON null.
func TestDecodeRowsHandlesSpecialFloats(t *testing.T) {
	data := []byte(`[{"g":"b","x":NaN},{"g":"c","x":Infinity},{"g":"d","x":-Infinity},{"g":"e","x":null}]`)
	rows, err := decodeRows(data)
	if err != nil {
		t.Fatalf("decodeRows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	wantText := []string{"NaN", "Inf", "-Inf"}
	for i, want := range wantText {
		if got := rows[i]["x"]; got != want {
			t.Errorf("row %d x = %v (%T), want string %q", i, got, got, want)
		}
	}
	if rows[3]["x"] != nil {
		t.Errorf("null row x = %v, want nil", rows[3]["x"])
	}
}

// TestColumnOrderHandlesSpecialFloats confirms the key-order scan also tolerates
// special-float values (it tokenizes the same bytes).
func TestColumnOrderHandlesSpecialFloats(t *testing.T) {
	data := []byte(`[{"g":"b","x":NaN}]`)
	cols, err := columnOrder(data)
	if err != nil {
		t.Fatalf("columnOrder: %v", err)
	}
	if want := []string{"g", "x"}; !reflect.DeepEqual(cols, want) {
		t.Errorf("cols = %v, want %v", cols, want)
	}
}
