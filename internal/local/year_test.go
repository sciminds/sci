package local

import "testing"

// ParseYear is the fast path that both scan sites (List/Search via
// scanListRow, Read directly) use to derive a clean int year from the
// dual-encoded "YYYY-MM-DD originalText" Zotero date string. Table-
// driven because the input shapes are varied in real libraries — four
// digits, dual-encoded, year-only padded with "00", and malformed.
func TestParseYear(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"2024-03-15 March 15, 2024", 2024},
		{"2023-00-00 2023", 2023}, // year-only padded form
		{"2023", 2023},            // bare year (also appears in Zotero)
		{"1871-00-00 1871", 1871},
		{"", 0},
		{"abc", 0},
		{"20", 0},   // too short
		{"0000", 0}, // rejected
		{"not-a-year", 0},
	}
	for _, c := range cases {
		if got := ParseYear(c.in); got != c.want {
			t.Errorf("ParseYear(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// Fixture items (10: date "2024-03-15 …", 30: date "2023") both pass
// through List's scanListRow; verify Year is populated end-to-end.
func TestList_PopulatesYear(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]int{}
	for _, it := range items {
		byKey[it.Key] = it.Year
	}
	if byKey["AAAA1111"] != 2024 {
		t.Errorf("AAAA1111 Year = %d, want 2024", byKey["AAAA1111"])
	}
	if byKey["CCCC3333"] != 2023 {
		t.Errorf("CCCC3333 Year = %d, want 2023", byKey["CCCC3333"])
	}
}

func TestRead_PopulatesYear(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if it.Year != 2024 {
		t.Errorf("AAAA1111 Year = %d, want 2024", it.Year)
	}
	if it.Date != "2024-03-15 March 15, 2024" {
		t.Errorf("Date preserved unexpectedly: %q", it.Date)
	}
}
