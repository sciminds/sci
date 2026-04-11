package hygiene

import (
	"strings"
	"unicode"
)

// NormalizeTitle prepares a title string for equality-bucketing and fuzzy
// comparison. It lowercases, replaces every non-letter/non-digit rune with
// a space, and collapses runs of whitespace into single spaces.
//
// Unicode letters and digits are preserved so accented titles match each
// other without losing identity (café → café, not cafe). Punctuation,
// symbols, and dashes all become spaces, which folds "Deep Learning: A
// Review" into "deep learning a review".
func NormalizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	// Collapse whitespace runs.
	return strings.Join(strings.Fields(b.String()), " ")
}
