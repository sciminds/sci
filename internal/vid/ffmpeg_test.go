package vid

import (
	"math"
	"testing"
)

func TestParseFraction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"standard fps", "30000/1001", 29.97},
		{"exact 30", "30/1", 30.0},
		{"exact 24", "24/1", 24.0},
		{"film fps", "24000/1001", 23.98},
		{"60fps", "60/1", 60.0},
		{"zero denominator", "30/0", 0},
		{"no slash", "30", 0},
		{"empty", "", 0},
		{"garbage", "abc/def", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFraction(tt.input)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("parseFraction(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
