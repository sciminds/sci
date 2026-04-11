package board

import (
	"math/rand/v2"
	"sort"
	"testing"
)

func TestBetween(t *testing.T) {
	cases := []struct {
		name        string
		left, right float64
		want        float64
	}{
		{"empty list", 0, 0, 1.0},
		{"prepend", 0, 2.0, 1.0},
		{"append", 2.0, 0, 3.0},
		{"middle", 1.0, 2.0, 1.5},
		{"tight middle", 1.0, 1.5, 1.25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Between(tc.left, tc.right)
			if got != tc.want {
				t.Errorf("Between(%v, %v) = %v, want %v", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

func TestNeedsNormalize(t *testing.T) {
	cases := []struct {
		name string
		pos  []float64
		want bool
	}{
		{"empty", nil, false},
		{"single", []float64{1.0}, false},
		{"well spaced", []float64{1.0, 2.0, 3.0}, false},
		{"gap below epsilon", []float64{1.0, 1.0 + 1e-10, 2.0}, true},
		{"zero", []float64{0, 1.0}, true},
		{"negative", []float64{-0.5, 1.0}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NeedsNormalize(tc.pos)
			if got != tc.want {
				t.Errorf("NeedsNormalize(%v) = %v, want %v", tc.pos, got, tc.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize([]float64{0.001, 0.002, 0.003, 5.0})
	want := []float64{1.0, 2.0, 3.0, 4.0}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestNormalize_Empty(t *testing.T) {
	if got := Normalize(nil); len(got) != 0 {
		t.Errorf("Normalize(nil) = %v, want empty", got)
	}
}

// TestBetween_PropertyNoDuplicates exercises Between across 1000 random
// insertions into a growing sorted list. Every result must be strictly
// between its neighbors, positive, and unique across the full sequence.
func TestBetween_PropertyNoDuplicates(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 42))
	positions := []float64{Between(0, 0)}
	for i := 0; i < 1000; i++ {
		idx := rng.IntN(len(positions) + 1)
		var left, right float64
		if idx > 0 {
			left = positions[idx-1]
		}
		if idx < len(positions) {
			right = positions[idx]
		}
		p := Between(left, right)
		if p <= 0 {
			t.Fatalf("iter %d: non-positive position %v", i, p)
		}
		if left > 0 && p <= left {
			t.Fatalf("iter %d: p=%v not > left=%v", i, p, left)
		}
		if right > 0 && p >= right {
			t.Fatalf("iter %d: p=%v not < right=%v", i, p, right)
		}
		positions = append(positions, p)
		sort.Float64s(positions)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] == positions[i-1] {
			t.Fatalf("duplicate at %d: %v", i, positions[i])
		}
	}
}
