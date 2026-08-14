package hygiene

// levenshteinCapped is a short-circuiting edit distance over rune slices.
// It returns the true distance if it is ≤ cap, or any value > cap to
// signal "exceeded". Callers that only care about a threshold can avoid
// the full DP: the inner loop aborts as soon as the minimum value in a
// row exceeds cap, at which point no subsequent cell can come in under.
//
// Passing cap = math.MaxInt makes it a plain Levenshtein distance, which is
// how similarity_test.go's reference implementation checks it — the whole
// point of a capped variant is that it agrees with the uncapped one
// everywhere the cap is not binding.
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
			curr[j] = min(del, ins, sub)
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
