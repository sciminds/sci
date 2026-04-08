package vid

import (
	"fmt"
	"testing"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		// plain seconds
		{"90", 90},
		{"0", 0},
		{"1.5", 1.5},
		{"0.25", 0.25},

		// MM:SS
		{"1:30", 90},
		{"0:05", 5},
		{"10:00", 600},

		// HH:MM:SS
		{"1:30:00", 5400},
		{"0:01:30", 90},
		{"1:00:00", 3600},
		{"2:30:45", 9045},
		{"0:00:30.5", 30.5},

		// hms style
		{"1h30m15s", 5415},
		{"30m", 1800},
		{"45s", 45},
		{"1h", 3600},
		{"1h30s", 3630},
		{"2m30s", 150},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTime(tt.input)
			if err != nil {
				t.Fatalf("ParseTime(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTimeErrors(t *testing.T) {
	bad := []string{"", "abc", ":::", "1h2x"}
	for _, input := range bad {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTime(input)
			if err == nil {
				t.Errorf("ParseTime(%q) should error", input)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		secs float64
		want string
	}{
		{0, "0:00"},
		{5, "0:05"},
		{90, "1:30"},
		{3661, "1:01:01"},
		{7200, "2:00:00"},
	}

	for _, tt := range tests {
		got := FormatTime(tt.secs)
		if got != tt.want {
			t.Errorf("FormatTime(%v) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func TestFormatTimeFilename(t *testing.T) {
	tests := []struct {
		secs float64
		want string
	}{
		{0, "0m00s"},
		{90, "1m30s"},
		{3661, "1h01m01s"},
		{45, "0m45s"},
	}

	for _, tt := range tests {
		got := FormatTimeFilename(tt.secs)
		if got != tt.want {
			t.Errorf("FormatTimeFilename(%v) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func ExampleParseTime() {
	secs, _ := ParseTime("1:30")
	fmt.Println(secs)
	secs, _ = ParseTime("1h30m15s")
	fmt.Println(secs)
	// Output:
	// 90
	// 5415
}

func ExampleFormatTime() {
	fmt.Println(FormatTime(90))
	fmt.Println(FormatTime(3661))
	// Output:
	// 1:30
	// 1:01:01
}
