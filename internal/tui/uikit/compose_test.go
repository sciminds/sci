package uikit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// ── Spread ─────────────────────────────────────────────────────────────────

func TestSpread(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		width       int
		left, right string
		wantWidth   int
		wantExact   string
	}{
		{
			name:      "basic left+right",
			width:     20,
			left:      "abc",
			right:     "xyz",
			wantWidth: 20,
			wantExact: "abc" + strings.Repeat(" ", 14) + "xyz",
		},
		{
			name:      "tight fit no gap",
			width:     6,
			left:      "abc",
			right:     "xyz",
			wantExact: "abc", // gap=0 → drop right
		},
		{
			name:      "overflow drops right",
			width:     5,
			left:      "abcdef",
			right:     "xyz",
			wantExact: "abcdef",
		},
		{
			name:      "zero width returns left",
			width:     0,
			left:      "abc",
			right:     "xyz",
			wantExact: "abc",
		},
		{
			name:      "empty left",
			width:     10,
			left:      "",
			right:     "xyz",
			wantExact: strings.Repeat(" ", 7) + "xyz",
		},
		{
			name:      "empty right",
			width:     10,
			left:      "abc",
			right:     "",
			wantExact: "abc" + strings.Repeat(" ", 7),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Spread(tt.width, tt.left, tt.right)
			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("Spread(%d, %q, %q) =\n  %q\nwant:\n  %q",
					tt.width, tt.left, tt.right, got, tt.wantExact)
			}
			if tt.wantWidth > 0 && lipgloss.Width(got) != tt.wantWidth {
				t.Errorf("Spread(%d, %q, %q) width = %d, want %d",
					tt.width, tt.left, tt.right, lipgloss.Width(got), tt.wantWidth)
			}
		})
	}
}

func TestSpreadMinGap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		width       int
		minGap      int
		left, right string
		wantWidth   int
		wantExact   string
	}{
		{
			name:      "plenty of room",
			width:     20,
			minGap:    1,
			left:      "abc",
			right:     "xyz",
			wantWidth: 20,
			wantExact: "abc" + strings.Repeat(" ", 14) + "xyz",
		},
		{
			name:      "exactly at min gap",
			width:     7,
			minGap:    1,
			left:      "abc",
			right:     "xyz",
			wantExact: "abc xyz",
		},
		{
			name:      "truncates left to make room",
			width:     10,
			minGap:    1,
			left:      "abcdefghij",
			right:     "xyz",
			wantWidth: 10,
		},
		{
			name:      "right too wide returns left",
			width:     3,
			minGap:    1,
			left:      "abc",
			right:     "xyz",
			wantExact: "abc",
		},
		{
			name:      "min gap clamped to 1",
			width:     7,
			minGap:    0,
			left:      "abc",
			right:     "xyz",
			wantExact: "abc xyz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SpreadMinGap(tt.width, tt.minGap, tt.left, tt.right)
			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("SpreadMinGap(%d, %d, %q, %q) =\n  %q\nwant:\n  %q",
					tt.width, tt.minGap, tt.left, tt.right, got, tt.wantExact)
			}
			if tt.wantWidth > 0 && lipgloss.Width(got) != tt.wantWidth {
				t.Errorf("SpreadMinGap(%d, %d, %q, %q) width = %d, want %d",
					tt.width, tt.minGap, tt.left, tt.right, lipgloss.Width(got), tt.wantWidth)
			}
		})
	}
}

// ── Center ─────────────────────────────────────────────────────────────────

func TestCenter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		width     int
		input     string
		wantWidth int
	}{
		{"centers short text", 20, "hi", 20},
		{"wide content unchanged", 3, "abcdef", 6},
		{"exact width", 5, "hello", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Center(tt.width, tt.input)
			gotW := lipgloss.Width(got)
			if gotW != tt.wantWidth {
				t.Errorf("Center(%d, %q) width = %d, want %d; got %q",
					tt.width, tt.input, gotW, tt.wantWidth, got)
			}
			if !strings.Contains(got, tt.input) {
				t.Errorf("Center(%d, %q) = %q, missing input", tt.width, tt.input, got)
			}
		})
	}
}

// ── Pad ────────────────────────────────────────────────────────────────────

func TestPadRight(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{"pads short", "ab", 5, "ab   "},
		{"exact width", "abc", 3, "abc"},
		{"wider than width", "abcdef", 3, "abcdef"},
		{"empty string", "", 4, "    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PadRight(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("PadRight(%q, %d) = %q, want %q",
					tt.input, tt.width, got, tt.want)
			}
		})
	}
}

func TestPadLeft(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{"pads short", "ab", 5, "   ab"},
		{"exact width", "abc", 3, "abc"},
		{"wider than width", "abcdef", 3, "abcdef"},
		{"empty string", "", 4, "    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PadLeft(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("PadLeft(%q, %d) = %q, want %q",
					tt.input, tt.width, got, tt.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	t.Parallel()
	t.Run("left alignment", func(t *testing.T) {
		got := Pad("ab", 5, lipgloss.Left)
		if got != "ab   " {
			t.Errorf("Pad left = %q, want %q", got, "ab   ")
		}
	})
	t.Run("right alignment", func(t *testing.T) {
		got := Pad("ab", 5, lipgloss.Right)
		if got != "   ab" {
			t.Errorf("Pad right = %q, want %q", got, "   ab")
		}
	})
	t.Run("center alignment", func(t *testing.T) {
		got := Pad("ab", 6, lipgloss.Center)
		if lipgloss.Width(got) != 6 {
			t.Errorf("Pad center width = %d, want 6", lipgloss.Width(got))
		}
		if !strings.Contains(got, "ab") {
			t.Errorf("Pad center = %q, missing content", got)
		}
	})
}

// ── Fit ────────────────────────────────────────────────────────────────────

func TestFit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		width     int
		pos       lipgloss.Position
		wantWidth int
	}{
		{"short left", "ab", 5, lipgloss.Left, 5},
		{"short right", "42", 5, lipgloss.Right, 5},
		{"exact", "hello", 5, lipgloss.Left, 5},
		{"truncates long", "abcdefghij", 5, lipgloss.Left, 5},
		{"zero width", "abc", 0, lipgloss.Left, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fit(tt.input, tt.width, tt.pos)
			gotW := lipgloss.Width(got)
			if gotW != tt.wantWidth {
				t.Errorf("Fit(%q, %d, %v) width = %d, want %d; got %q",
					tt.input, tt.width, tt.pos, gotW, tt.wantWidth, got)
			}
		})
	}
}

func TestFitRight(t *testing.T) {
	t.Parallel()
	got := FitRight("42", 6)
	if lipgloss.Width(got) != 6 {
		t.Errorf("FitRight width = %d, want 6", lipgloss.Width(got))
	}
	if !strings.HasSuffix(got, "42") {
		t.Errorf("FitRight(%q, 6) = %q, want suffix '42'", "42", got)
	}
}
