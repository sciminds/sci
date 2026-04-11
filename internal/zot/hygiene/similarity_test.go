package hygiene

import "testing"

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
		got := SimilarityRatio(c.a, c.b)
		if abs(got-c.want) > 1e-9 {
			t.Errorf("SimilarityRatio(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSimilarityRatio_Symmetric(t *testing.T) {
	t.Parallel()
	// Ratio must be symmetric to avoid clustering order artifacts.
	a := "deep learning for neuroimaging"
	b := "deep learning for neuro imaging"
	if SimilarityRatio(a, b) != SimilarityRatio(b, a) {
		t.Error("SimilarityRatio is not symmetric")
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
		r := SimilarityRatio(p[0], p[1])
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
		r := SimilarityRatio(p[0], p[1])
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

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
