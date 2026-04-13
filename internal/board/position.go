package board

import "github.com/samber/lo"

// Fractional indexing for stable, conflict-free ordering within a column.
//
// Each card carries a Position (float64 > 0). Inserting between two cards
// picks the midpoint of their positions, so concurrent inserts by different
// clients never collide — each gets its own ULID and fold order resolves
// ties deterministically. Repeated midpoint insertions can eventually shrink
// gaps below float64 precision; [NeedsNormalize] detects this and [Normalize]
// renumbers to evenly spaced integers, emitted as a single column.reorder
// event during Apply.

// positionEpsilon is the smallest gap we tolerate between adjacent positions
// before forcing a renormalization. Well above float64's resolution to leave
// headroom for a few more insertions before precision bites.
const positionEpsilon = 1e-9

// Between returns a new position strictly between left and right. A zero
// value for left means "no left neighbor" (empty list or prepend), and a
// zero value for right means "no right neighbor" (append).
//
// When both neighbors are present, Between assumes left < right — the caller
// is responsible for passing the correct pair from a sorted column.
func Between(left, right float64) float64 {
	switch {
	case left == 0 && right == 0:
		return 1.0
	case left == 0:
		return right / 2
	case right == 0:
		return left + 1.0
	default:
		return (left + right) / 2
	}
}

// NeedsNormalize reports whether positions have drifted into a state where
// [Normalize] should be called. It returns true if any position is
// non-positive, or if any adjacent gap (in the provided order) is below
// positionEpsilon. The caller is expected to pass positions in sorted order
// — Apply always does.
func NeedsNormalize(positions []float64) bool {
	for _, p := range positions {
		if p <= 0 {
			return true
		}
	}
	for i := 1; i < len(positions); i++ {
		if positions[i]-positions[i-1] < positionEpsilon {
			return true
		}
	}
	return false
}

// Normalize renumbers positions to evenly spaced integers (1.0, 2.0, 3.0, …).
// Order is preserved; the caller sorts the input first if needed.
func Normalize(positions []float64) []float64 {
	return lo.Times(len(positions), func(i int) float64 {
		return float64(i + 1)
	})
}
