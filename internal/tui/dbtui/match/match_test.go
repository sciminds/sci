package match

import (
	"testing"
)

// ────────────────────────────────────────────────────
// Fuzzy matching tests (backed by sahilm/fuzzy)
// ────────────────────────────────────────────────────

func TestFuzzyNoMatch(t *testing.T) {
	score, positions := Fuzzy("xyz", "hello")
	if score != 0 {
		t.Errorf("expected score 0, got %d", score)
	}
	if positions != nil {
		t.Errorf("expected nil positions, got %v", positions)
	}
}

func TestFuzzyPrefix(t *testing.T) {
	score, positions := Fuzzy("he", "hello")
	if score <= 0 {
		t.Errorf("expected positive score for prefix match, got %d", score)
	}
	if len(positions) != 2 {
		t.Errorf("expected 2 positions, got %v", positions)
	}
	if positions[0] != 0 || positions[1] != 1 {
		t.Errorf("expected positions [0,1], got %v", positions)
	}
}

func TestFuzzySubstring(t *testing.T) {
	score, positions := Fuzzy("ell", "hello")
	if score <= 0 {
		t.Errorf("expected positive score for substring match, got %d", score)
	}
	if len(positions) != 3 {
		t.Errorf("expected 3 positions, got %v", positions)
	}
}

func TestFuzzyPrefixBeatsLate(t *testing.T) {
	scorePrefix, _ := Fuzzy("he", "hello world")
	scoreLate, _ := Fuzzy("wo", "hello world")
	if scorePrefix <= 0 || scoreLate <= 0 {
		t.Errorf("both should match: prefix=%d, late=%d", scorePrefix, scoreLate)
	}
	if scorePrefix < scoreLate {
		t.Errorf("expected prefix score (%d) >= late score (%d)", scorePrefix, scoreLate)
	}
}

func TestFuzzyEmptyQuery(t *testing.T) {
	score, positions := Fuzzy("", "anything")
	if score <= 0 {
		t.Errorf("expected positive score for empty query, got %d", score)
	}
	if positions != nil {
		t.Errorf("expected nil positions for empty query, got %v", positions)
	}
}

func TestFuzzyQueryLongerThanTarget(t *testing.T) {
	score, positions := Fuzzy("toolong", "hi")
	if score != 0 {
		t.Errorf("expected score 0, got %d", score)
	}
	if positions != nil {
		t.Errorf("expected nil positions, got %v", positions)
	}
}

func TestFuzzyCaseInsensitive(t *testing.T) {
	score, _ := Fuzzy("HE", "hello")
	if score <= 0 {
		t.Errorf("expected case-insensitive match, got score %d", score)
	}
}

func TestFuzzyNonContiguous(t *testing.T) {
	score, positions := Fuzzy("hlo", "hello")
	if score <= 0 {
		t.Errorf("expected positive score for non-contiguous match, got %d", score)
	}
	if len(positions) != 3 {
		t.Errorf("expected 3 positions, got %v", positions)
	}
}

func TestFuzzyWordBoundary(t *testing.T) {
	// "fb" should match "foo_bar" at word boundaries
	score, positions := Fuzzy("fb", "foo_bar")
	if score <= 0 {
		t.Errorf("expected word-boundary match, got score %d", score)
	}
	if len(positions) != 2 {
		t.Errorf("expected 2 positions, got %v", positions)
	}
}

func TestFuzzyExactMatch(t *testing.T) {
	score, positions := Fuzzy("hello", "hello")
	if score <= 0 {
		t.Errorf("expected positive score for exact match, got %d", score)
	}
	if len(positions) != 5 {
		t.Errorf("expected 5 positions, got %v", positions)
	}
}

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
