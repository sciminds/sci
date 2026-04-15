package match

import (
	"testing"
)

// ────────────────────────────────────────────────────
// Search query parsing tests
// ────────────────────────────────────────────────────

// helper: flatten single-group results for simple tests.
func firstGroup(t *testing.T, groups [][]Clause) []Clause {
	t.Helper()
	if len(groups) != 1 {
		t.Fatalf("expected 1 OR group, got %d: %+v", len(groups), groups)
	}
	return groups[0]
}

func TestParseQueryPlain(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("hello world"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Column != "" {
		t.Errorf("expected empty column, got %q", clauses[0].Column)
	}
	if clauses[0].Terms != "hello world" {
		t.Errorf("expected terms %q, got %q", "hello world", clauses[0].Terms)
	}
}

func TestParseQueryColumnScoped(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@name: alice"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Column != "name" || clauses[0].Terms != "alice" {
		t.Errorf("got %+v", clauses[0])
	}
}

func TestParseQueryColumnNoColon(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@name alice"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Column != "name" || clauses[0].Terms != "alice" {
		t.Errorf("got %+v", clauses[0])
	}
}

func TestParseQueryMultiClause(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@col1: val1, @col2: val2"))
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(clauses))
	}
	if clauses[0].Column != "col1" || clauses[0].Terms != "val1" {
		t.Errorf("clause 0: got %+v", clauses[0])
	}
	if clauses[1].Column != "col2" || clauses[1].Terms != "val2" {
		t.Errorf("clause 1: got %+v", clauses[1])
	}
}

func TestParseQueryMultiClauseSpaceSeparated(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@department: Bio @location: 210"))
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d: %+v", len(clauses), clauses)
	}
	if clauses[0].Column != "department" || clauses[0].Terms != "Bio" {
		t.Errorf("clause 0: got %+v", clauses[0])
	}
	if clauses[1].Column != "location" || clauses[1].Terms != "210" {
		t.Errorf("clause 1: got %+v", clauses[1])
	}
}

func TestParseQueryMultiClauseNoColonSpaceSeparated(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@name alice @city paris"))
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d: %+v", len(clauses), clauses)
	}
	if clauses[0].Column != "name" || clauses[0].Terms != "alice" {
		t.Errorf("clause 0: got %+v", clauses[0])
	}
	if clauses[1].Column != "city" || clauses[1].Terms != "paris" {
		t.Errorf("clause 1: got %+v", clauses[1])
	}
}

func TestParseQueryEmpty(t *testing.T) {
	groups := ParseClauses("")
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for empty query, got %d", len(groups))
	}
}

func TestParseQueryColumnOnly(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@name"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Column != "name" {
		t.Errorf("expected column %q, got %q", "name", clauses[0].Column)
	}
	if clauses[0].Terms != "" {
		t.Errorf("expected empty terms, got %q", clauses[0].Terms)
	}
}

// ParseQuery is a convenience wrapper — test backward compat.
func TestParseQueryCompat(t *testing.T) {
	col, terms := ParseQuery("@name: alice")
	if col != "name" {
		t.Errorf("expected column %q, got %q", "name", col)
	}
	if terms != "alice" {
		t.Errorf("expected terms %q, got %q", "alice", terms)
	}
}

// ────────────────────────────────────────────────────
// OR group tests
// ────────────────────────────────────────────────────

func TestParseClausesOR(t *testing.T) {
	groups := ParseClauses("@col1: val1 | @col2: val2")
	if len(groups) != 2 {
		t.Fatalf("expected 2 OR groups, got %d: %+v", len(groups), groups)
	}
	if len(groups[0]) != 1 || groups[0][0].Column != "col1" || groups[0][0].Terms != "val1" {
		t.Errorf("group 0: got %+v", groups[0])
	}
	if len(groups[1]) != 1 || groups[1][0].Column != "col2" || groups[1][0].Terms != "val2" {
		t.Errorf("group 1: got %+v", groups[1])
	}
}

func TestParseClausesORWithAND(t *testing.T) {
	// Each OR branch has AND clauses within it.
	groups := ParseClauses("@a: 1 @b: 2 | @c: 3")
	if len(groups) != 2 {
		t.Fatalf("expected 2 OR groups, got %d: %+v", len(groups), groups)
	}
	// First OR group: @a: 1 AND @b: 2
	if len(groups[0]) != 2 {
		t.Fatalf("group 0: expected 2 AND clauses, got %d: %+v", len(groups[0]), groups[0])
	}
	if groups[0][0].Column != "a" || groups[0][0].Terms != "1" {
		t.Errorf("group 0 clause 0: got %+v", groups[0][0])
	}
	if groups[0][1].Column != "b" || groups[0][1].Terms != "2" {
		t.Errorf("group 0 clause 1: got %+v", groups[0][1])
	}
	// Second OR group: @c: 3
	if len(groups[1]) != 1 || groups[1][0].Column != "c" || groups[1][0].Terms != "3" {
		t.Errorf("group 1: got %+v", groups[1])
	}
}

func TestParseClausesORPlainText(t *testing.T) {
	groups := ParseClauses("alice | bob")
	if len(groups) != 2 {
		t.Fatalf("expected 2 OR groups, got %d: %+v", len(groups), groups)
	}
	if groups[0][0].Terms != "alice" {
		t.Errorf("group 0: got %+v", groups[0])
	}
	if groups[1][0].Terms != "bob" {
		t.Errorf("group 1: got %+v", groups[1])
	}
}

// ────────────────────────────────────────────────────
// Negate tests
// ────────────────────────────────────────────────────

func TestParseClausesNegate(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@col: -val"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Column != "col" || clauses[0].Terms != "val" || !clauses[0].Negate {
		t.Errorf("expected negated clause {col, val, true}, got %+v", clauses[0])
	}
}

func TestParseClausesNegateNoColumn(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("-bob"))
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	if clauses[0].Terms != "bob" || !clauses[0].Negate {
		t.Errorf("expected negated clause {bob, true}, got %+v", clauses[0])
	}
}

func TestParseClausesNegateWithAND(t *testing.T) {
	clauses := firstGroup(t, ParseClauses("@name: alice @city: -london"))
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(clauses))
	}
	if clauses[0].Negate {
		t.Errorf("clause 0 should not be negated: %+v", clauses[0])
	}
	if !clauses[1].Negate || clauses[1].Column != "city" || clauses[1].Terms != "london" {
		t.Errorf("clause 1: expected negated {city, london}, got %+v", clauses[1])
	}
}

func TestParseClausesNegateWithOR(t *testing.T) {
	groups := ParseClauses("@col: -val1 | @col: val2")
	if len(groups) != 2 {
		t.Fatalf("expected 2 OR groups, got %d", len(groups))
	}
	if !groups[0][0].Negate || groups[0][0].Terms != "val1" {
		t.Errorf("group 0: expected negated, got %+v", groups[0][0])
	}
	if groups[1][0].Negate || groups[1][0].Terms != "val2" {
		t.Errorf("group 1: expected non-negated, got %+v", groups[1][0])
	}
}

func TestParseClausesBareHyphenNotNegate(t *testing.T) {
	// A bare "-" with nothing after it is not a negate — it's an empty search.
	clauses := firstGroup(t, ParseClauses("@col: -"))
	if clauses[0].Negate {
		t.Errorf("bare hyphen should not be treated as negate: %+v", clauses[0])
	}
}

// ────────────────────────────────────────────────────
// MatchRow tests — token-AND-across-row substring matching
// ────────────────────────────────────────────────────

func TestMatchRowSingleTokenSingleCell(t *testing.T) {
	hl, ok := MatchRow([]string{"gossip"}, []string{"Gossip drives vicarious learning"}, -1)
	if !ok {
		t.Fatal("expected match")
	}
	want := []int{0, 1, 2, 3, 4, 5}
	got := hl[0]
	if len(got) != len(want) {
		t.Fatalf("positions len = %d, want %d: %v", len(got), len(want), got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pos[%d] = %d, want %d", i, got[i], p)
		}
	}
}

func TestMatchRowMultiTokenSameCell(t *testing.T) {
	hl, ok := MatchRow(
		[]string{"gossip", "drives"},
		[]string{"Gossip drives vicarious learning"},
		-1,
	)
	if !ok {
		t.Fatal("expected match")
	}
	// "gossip" at runes 0..5, "drives" at runes 7..12
	want := []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 10, 11, 12}
	got := hl[0]
	if len(got) != len(want) {
		t.Fatalf("positions = %v, want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pos[%d] = %d, want %d (got %v)", i, got[i], p, got)
		}
	}
}

func TestMatchRowMultiTokenCrossCell(t *testing.T) {
	// Token "gossip" in cell 0, "drives" in cell 3. Should still match.
	hl, ok := MatchRow(
		[]string{"gossip", "drives"},
		[]string{"A study of gossip", "", "", "what drives sharing"},
		-1,
	)
	if !ok {
		t.Fatal("expected match across cells")
	}
	if len(hl[0]) == 0 {
		t.Errorf("expected highlights in cell 0: %v", hl)
	}
	if len(hl[3]) == 0 {
		t.Errorf("expected highlights in cell 3: %v", hl)
	}
}

func TestMatchRowMissingToken(t *testing.T) {
	_, ok := MatchRow(
		[]string{"gossip", "nonexistent"},
		[]string{"A study of gossip", "some other cell"},
		-1,
	)
	if ok {
		t.Error("expected no match when a token is missing")
	}
}

func TestMatchRowCaseInsensitive(t *testing.T) {
	_, ok := MatchRow([]string{"GOSSIP"}, []string{"gossip drives"}, -1)
	if !ok {
		t.Error("expected case-insensitive match")
	}
}

func TestMatchRowScopedColumn(t *testing.T) {
	cells := []string{"gossip is the target", "drives", "ignored"}
	// Scoped to col 1: "gossip" not present → no match.
	if _, ok := MatchRow([]string{"gossip"}, cells, 1); ok {
		t.Error("scoped match should have failed — gossip is in col 0, not col 1")
	}
	// Scoped to col 0: "gossip" present → match.
	hl, ok := MatchRow([]string{"gossip"}, cells, 0)
	if !ok {
		t.Fatal("expected scoped match in col 0")
	}
	if len(hl[0]) != 6 {
		t.Errorf("expected 6 highlight positions in col 0, got %v", hl)
	}
	if len(hl[1]) != 0 {
		t.Errorf("expected no highlights in col 1: %v", hl)
	}
}

func TestMatchRowEmptyTokens(t *testing.T) {
	if _, ok := MatchRow(nil, []string{"anything"}, -1); ok {
		t.Error("empty tokens should not match (caller's job to decide semantics)")
	}
	if _, ok := MatchRow([]string{}, []string{"anything"}, -1); ok {
		t.Error("empty tokens should not match")
	}
}

func TestMatchRowUnicodeRuneOffsets(t *testing.T) {
	// "café" is 4 runes but 5 bytes ("é" = 2 bytes). Highlight must use rune indices.
	hl, ok := MatchRow([]string{"fé"}, []string{"café latté"}, -1)
	if !ok {
		t.Fatal("expected match")
	}
	// "fé" starts at rune index 2, length 2 runes → positions [2,3].
	got := hl[0]
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("expected rune positions [2,3], got %v", got)
	}
}

func TestMatchRowMultipleOccurrencesSameCell(t *testing.T) {
	// "ab" appears twice in "ababab" — both spans should be in highlights.
	hl, ok := MatchRow([]string{"ab"}, []string{"ababab"}, -1)
	if !ok {
		t.Fatal("expected match")
	}
	// Three "ab" hits at 0, 2, 4 → positions [0,1,2,3,4,5].
	want := []int{0, 1, 2, 3, 4, 5}
	got := hl[0]
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pos[%d] = %d, want %d", i, got[i], p)
		}
	}
}

func TestMatchRowOverlappingTokensDeduped(t *testing.T) {
	// Tokens "ab" and "abc" both hit "abcdef" — overlapping spans must be deduped and sorted.
	hl, ok := MatchRow([]string{"ab", "abc"}, []string{"abcdef"}, -1)
	if !ok {
		t.Fatal("expected match")
	}
	want := []int{0, 1, 2} // union of [0,1] and [0,1,2]
	got := hl[0]
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pos[%d] = %d, want %d", i, got[i], p)
		}
	}
}

func TestMatchRowEmptyCellsNoPanic(t *testing.T) {
	_, _ = MatchRow([]string{"x"}, []string{"", "", ""}, -1)
	// Just asserting no panic and no spurious match.
	_, ok := MatchRow([]string{"x"}, []string{"", "", ""}, -1)
	if ok {
		t.Error("empty cells should not match non-empty token")
	}
}

func TestMatchRowSubstringInMiddle(t *testing.T) {
	// Substring match, not prefix: "idget" should hit "Widget".
	hl, ok := MatchRow([]string{"idget"}, []string{"Widget"}, -1)
	if !ok {
		t.Fatal("expected substring match")
	}
	// "idget" at runes 1..5.
	want := []int{1, 2, 3, 4, 5}
	got := hl[0]
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pos[%d] = %d, want %d", i, got[i], p)
		}
	}
}

func TestMatchRowNonContiguousIsNOTMatched(t *testing.T) {
	// Deliberate regression guard: fuzzy-style non-contiguous chars must NOT match
	// under the new substring semantics. "gdrives" should miss "gossip drives".
	if _, ok := MatchRow([]string{"gdrives"}, []string{"gossip drives"}, -1); ok {
		t.Error("substring match must not be fuzzy — 'gdrives' should not match 'gossip drives'")
	}
}
