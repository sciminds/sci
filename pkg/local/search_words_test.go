package local

// Tests for free-text word semantics: multi-word free text ANDs per
// word across fields (instead of one literal substring), bare year
// tokens filter by year, and quoted phrases stay literal substrings.
// Fixture reference: AAAA1111 "Deep Learning for Neuroimaging" (Smith,
// 2024), CCCC3333 "A Book About Cats" (Smith, 2023), GGGG7777
// "Attention Mechanisms in Cortical Networks" (2023).

import (
	"slices"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/tui/dbtui/match"
)

func searchKeys(t *testing.T, db *DB, query string) []string {
	t.Helper()
	items, err := db.Search(query, 50)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	return lo.Map(items, func(it Item, _ int) string { return it.Key })
}

func TestSearch_WordsANDAcrossFields(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "smith" lives in creators, "neuroimaging" in the title — no single
	// column contains the literal substring "smith neuroimaging".
	got := searchKeys(t, db, "smith neuroimaging")
	if !slices.Equal(got, []string{"AAAA1111"}) {
		t.Errorf("smith neuroimaging = %v, want [AAAA1111]", got)
	}
}

func TestSearch_BareYearTokenFiltersByYear(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "smith 2023": creator Smith is on AAAA1111 (2024) and CCCC3333
	// (2023) — the year token must narrow to the 2023 one.
	got := searchKeys(t, db, "smith 2023")
	if !slices.Equal(got, []string{"CCCC3333"}) {
		t.Errorf("smith 2023 = %v, want [CCCC3333]", got)
	}

	// A lone year means "papers from that year", not "fields containing
	// these four digits".
	got = searchKeys(t, db, "2023")
	slices.Sort(got)
	if !slices.Equal(got, []string{"CCCC3333", "GGGG7777"}) {
		t.Errorf("2023 = %v, want [CCCC3333 GGGG7777]", got)
	}
}

func TestSearch_QuotedPhraseIsLiteral(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// The quotes mark a phrase: strip them for the metadata match (the
	// raw quote characters can never appear in a title) but keep the
	// span as one substring.
	got := searchKeys(t, db, `"deep learning"`)
	if !slices.Equal(got, []string{"AAAA1111"}) {
		t.Errorf(`"deep learning" = %v, want [AAAA1111]`, got)
	}

	// Reversed word order inside quotes must NOT match — that's the
	// whole difference between a phrase and word-AND.
	if got := searchKeys(t, db, `"learning deep"`); len(got) != 0 {
		t.Errorf(`"learning deep" = %v, want no hits`, got)
	}

	// Quoting a year escapes the year rewrite: "2023" is then a literal
	// substring, which no metadata field contains.
	if got := searchKeys(t, db, `"2023"`); len(got) != 0 {
		t.Errorf(`quoted "2023" = %v, want no hits (dates are not free-text fields)`, got)
	}
}

func TestSearch_SmartcasePerToken(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Each word is its own clause, so smartcase applies per token:
	// "Smith" (uppercase → case-sensitive) still matches creator
	// "Alice Smith" while the year token narrows.
	got := searchKeys(t, db, "Smith 2023")
	if !slices.Equal(got, []string{"CCCC3333"}) {
		t.Errorf("Smith 2023 = %v, want [CCCC3333]", got)
	}
}

func TestExpandFreeText(t *testing.T) {
	t.Parallel()
	free := func(terms string) match.Clause { return match.Clause{Terms: terms} }

	cases := []struct {
		name string
		in   []match.Clause
		want []match.Clause
	}{
		{
			"multi-word splits",
			[]match.Clause{free("jolly gossip")},
			[]match.Clause{free("jolly"), free("gossip")},
		},
		{
			"bare year becomes a year clause",
			[]match.Clause{free("jolly 2021")},
			[]match.Clause{free("jolly"), {Column: "year", Terms: "2021"}},
		},
		{
			"quoted phrase stays one clause, quotes preserved",
			[]match.Clause{free(`"prediction error" jolly`)},
			[]match.Clause{free(`"prediction error"`), free("jolly")},
		},
		{
			"quoted year is not rewritten",
			[]match.Clause{free(`"2021"`)},
			[]match.Clause{free(`"2021"`)},
		},
		{
			"negated free text passes through unsplit",
			[]match.Clause{{Terms: "deep learning", Negate: true}},
			[]match.Clause{{Terms: "deep learning", Negate: true}},
		},
		{
			"field clauses untouched",
			[]match.Clause{{Column: "author", Terms: "jolly smith"}},
			[]match.Clause{{Column: "author", Terms: "jolly smith"}},
		},
		{
			"single word passes through",
			[]match.Clause{free("neuroimaging")},
			[]match.Clause{free("neuroimaging")},
		},
		{
			"out-of-range number stays free text",
			[]match.Clause{free("route 66 highway 2101")},
			[]match.Clause{free("route"), free("66"), free("highway"), free("2101")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := expandFreeText(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Errorf("expandFreeText(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBareSearchText_ExcludesYearRewrites(t *testing.T) {
	t.Parallel()
	// The content index should be asked about "jolly", not "jolly 2021"
	// — the year is a metadata filter, not paper-text evidence.
	group := expandFreeText([]match.Clause{{Terms: "jolly 2021"}})
	if got := bareSearchText(group); got != "jolly" {
		t.Errorf("bareSearchText = %q, want %q", got, "jolly")
	}
	// Phrases keep their quotes so the content index sees the phrase.
	group = expandFreeText([]match.Clause{{Terms: `"prediction error" 2021`}})
	if got := bareSearchText(group); got != `"prediction error"` {
		t.Errorf("bareSearchText = %q, want quoted phrase", got)
	}
}
