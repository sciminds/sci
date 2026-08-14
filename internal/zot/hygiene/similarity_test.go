package hygiene

import (
	"math"
	"testing"
)

// ── Reference implementation ────────────────────────────────────────────────
//
// levenshteinRunes and similarityRatio are the textbook forms, kept here and
// nowhere else. Production uses levenshteinCapped, which short-circuits once
// the distance exceeds a budget, and recomputes the ratio inline against that
// budget's maxLen — an optimization worth having and worth checking, since a
// bug in the short circuit would silently under-report duplicates rather than
// fail loudly.
//
// So these are an oracle, not dead code: TestLevenshteinCapped_MatchesUncapped
// is the reason they exist, and the threshold cases below pin what the 0.85
// clustering cutoff means in terms of real titles.

// levenshteinRunes computes edit distance between two rune slices using the
// standard O(n*m) DP with a rolling two-row buffer.
func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// levenshtein is the string-flavored wrapper the table tests read better with.
func levenshtein(a, b string) int {
	return levenshteinRunes([]rune(a), []rune(b))
}

// similarityRatio returns 1 - (levenshtein(a,b) / max(len(a), len(b))),
// clamped to [0,1]. Runes, so multi-byte characters count as single units.
// Two empty strings are perfectly similar (1.0) because they are identical.
// Symmetric by construction.
func similarityRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	maxLen := max(len(ra), len(rb))
	if maxLen == 0 {
		return 1.0
	}
	r := 1.0 - float64(levenshteinRunes(ra, rb))/float64(maxLen)
	return min(max(r, 0), 1)
}

// ── The differential test the oracle exists for ─────────────────────────────

// TestLevenshteinCapped_MatchesUncapped is the check that was missing: the
// capped implementation must agree with a plain Levenshtein everywhere the cap
// is not binding, and must report "exceeded" exactly when it is.
//
// Without this, levenshteinCapped's early exits — the row-minimum abort and
// the length-gap shortcut — were pinned only by five hand-computed
// expectations. A wrong abort makes duplicate detection quietly miss pairs,
// which is invisible in production: fewer findings looks like a clean library.
func TestLevenshteinCapped_MatchesUncapped(t *testing.T) {
	t.Parallel()
	corpus := []string{
		"", "a", "ab", "abc", "kitten", "sitting", "flaw", "lawn",
		"café", "cafe", "deep learning for neuroimaging",
		"deep learning for neuro imaging", "attention is all you need",
		"attention is all you need revisited", "a survey of graph neural networks",
		"a survery of graph neural networks",
	}
	for _, a := range corpus {
		for _, b := range corpus {
			ra, rb := []rune(a), []rune(b)
			want := levenshteinRunes(ra, rb)

			if got := levenshteinCapped(ra, rb, math.MaxInt); got != want {
				t.Errorf("levenshteinCapped(%q,%q,MaxInt) = %d, want %d", a, b, got, want)
			}

			// At every cap, the contract is: exact when within budget,
			// strictly greater than the cap when not.
			for cap := range want + 2 {
				got := levenshteinCapped(ra, rb, cap)
				if want <= cap {
					if got != want {
						t.Errorf("levenshteinCapped(%q,%q,%d) = %d, want exact %d", a, b, cap, got, want)
					}
				} else if got <= cap {
					t.Errorf("levenshteinCapped(%q,%q,%d) = %d, want >%d (true distance %d)", a, b, cap, got, cap, want)
				}
			}
		}
	}
}

// ── Reference-implementation properties ─────────────────────────────────────

func TestLevenshteinDistance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"a", "b", 1},
		{"café", "cafe", 1}, // unicode: one rune differs
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSimilarityRatio(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want float64
	}{
		{"", "", 1.0},       // both empty = identical
		{"abc", "abc", 1.0}, // identical
		{"abc", "xyz", 0.0}, // all different
		{"kitten", "sitting", 4.0 / 7.0},
	}
	for _, c := range cases {
		got := similarityRatio(c.a, c.b)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("similarityRatio(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSimilarityRatio_Symmetric(t *testing.T) {
	t.Parallel()
	// Ratio must be symmetric to avoid clustering order artifacts.
	a := "deep learning for neuroimaging"
	b := "deep learning for neuro imaging"
	if similarityRatio(a, b) != similarityRatio(b, a) {
		t.Error("similarityRatio is not symmetric")
	}
}

func TestSimilarityRatio_Threshold(t *testing.T) {
	t.Parallel()
	// Pairs that should cross a 0.85 threshold — genuine typo-level
	// variants of the same title. Anything that changes meaningful words
	// is intentionally NOT in this list; the title clusterer is for
	// catching near-duplicates, not semantic variants.
	pairs := [][2]string{
		{
			"deep learning for neuroimaging",
			"deep learning for neuro imaging", // space insertion
		},
		{
			"attention is all you need",
			"attention is all you needs", // single-char typo
		},
		{
			"a survey of graph neural networks",
			"a survery of graph neural networks", // transposition
		},
	}
	for _, p := range pairs {
		r := similarityRatio(p[0], p[1])
		if r < 0.85 {
			t.Errorf("expected near-duplicate ratio >= 0.85, got %v for %q / %q", r, p[0], p[1])
		}
	}

	// Pairs that should NOT cross the threshold — legitimately different
	// papers that share most words.
	nonDupes := [][2]string{
		{
			"attention is all you need",
			"attention is all you need revisited",
		},
	}
	for _, p := range nonDupes {
		r := similarityRatio(p[0], p[1])
		if r >= 0.85 {
			t.Errorf("unexpected match: %q / %q ratio %v crosses 0.85", p[0], p[1], r)
		}
	}
}

func TestLevenshteinCapped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		cap  int
		want int
	}{
		{"kitten", "sitting", 10, 3},                        // true distance
		{"kitten", "sitting", 3, 3},                         // equal to cap — still computed
		{"kitten", "sitting", 2, 3},                         // exceeds cap — sentinel
		{"abc", "xyz", 1, 2},                                // exceeds cap → returns >cap
		{"long text here", "totally different thing", 2, 3}, // length gap alone
	}
	for _, c := range cases {
		got := levenshteinCapped([]rune(c.a), []rune(c.b), c.cap)
		if c.want <= c.cap {
			if got != c.want {
				t.Errorf("capped(%q,%q,%d) = %d, want %d", c.a, c.b, c.cap, got, c.want)
			}
		} else {
			if got <= c.cap {
				t.Errorf("capped(%q,%q,%d) = %d, want >%d", c.a, c.b, c.cap, got, c.cap)
			}
		}
	}
}
