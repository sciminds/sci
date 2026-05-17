package duck

import "testing"

// TestInfoDuckDBMulti checks that Info enumerates every base table in a
// duckdb file and returns the right row + column counts.
func TestInfoDuckDBMulti(t *testing.T) {
	requireDuck(t)
	entries, err := Info(tinyDuck)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (extras, people): %+v", len(entries), entries)
	}
	byName := map[string]TableMeta{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	// tiny.csv has 3 data rows (id,name,score) → people has 3 rows / 3 cols.
	if got := byName["people"]; got.Rows != 3 || got.Columns != 3 || got.IsView {
		t.Errorf("people = %+v, want rows=3 cols=3 isview=false", got)
	}
	// extras has 2 rows (a,1)/(b,2) and 2 cols (k,v).
	if got := byName["extras"]; got.Rows != 2 || got.Columns != 2 || got.IsView {
		t.Errorf("extras = %+v, want rows=2 cols=2 isview=false", got)
	}
}

// TestInfoDuckDBSingle confirms Info works on a single-table file.
func TestInfoDuckDBSingle(t *testing.T) {
	requireDuck(t)
	entries, err := Info(singleDuck)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "only_one" {
		t.Fatalf("got %+v, want one entry named only_one", entries)
	}
	if entries[0].Rows != 3 || entries[0].Columns != 3 {
		t.Errorf("only_one = %+v, want rows=3 cols=3", entries[0])
	}
}
