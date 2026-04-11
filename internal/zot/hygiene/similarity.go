package hygiene

// SimilarityRatio returns 1 - (levenshtein(a,b) / max(len(a), len(b))),
// clamped to [0,1]. Both inputs are compared as rune slices so multi-byte
// characters count as single units. Two empty strings are defined as
// perfectly similar (1.0) because they are identical.
//
// This function is symmetric by construction.
func SimilarityRatio(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	maxLen := len(ra)
	if len(rb) > maxLen {
		maxLen = len(rb)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshteinRunes(ra, rb)
	r := 1.0 - float64(dist)/float64(maxLen)
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// levenshtein is a convenience wrapper that operates on strings. It exists
// mainly so tests can hit the distance function directly without rune
// conversion ceremony.
func levenshtein(a, b string) int {
	return levenshteinRunes([]rune(a), []rune(b))
}

// levenshteinCapped is a short-circuiting version of levenshteinRunes.
// It returns the true distance if it is ≤ cap, or any value > cap to
// signal "exceeded". Callers that only care about a threshold can avoid
// the full DP: the inner loop aborts as soon as the minimum value in a
// row exceeds cap, at which point no subsequent cell can come in under.
//
// Passing cap = math.MaxInt is equivalent to levenshteinRunes.
func levenshteinCapped(a, b []rune, cap int) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// A length gap bigger than the cap already violates the threshold.
	if abs := len(a) - len(b); abs > cap || -abs > cap {
		return cap + 1
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
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > cap {
			return cap + 1
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// levenshteinRunes computes edit distance between two rune slices using
// the standard O(n*m) DP with a rolling two-row buffer — O(min(n,m)) space.
func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// Ensure b is the shorter side so the row buffers are as small as possible.
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
			// deletion, insertion, substitution
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
