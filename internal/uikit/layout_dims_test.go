package uikit

import "testing"

func TestContentWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"zero", 0, 0},
		{"negative small", -1, 0},
		{"negative large", -100, 0},
		{"exactly padding", PageSidePadding, 0},
		{"one more than padding", PageSidePadding + 1, 1},
		{"80 columns", 80, 80 - PageSidePadding},
		{"120 columns", 120, 120 - PageSidePadding},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContentWidth(tt.input)
			if got != tt.want {
				t.Errorf("ContentWidth(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestOverlayBodyHeight(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		termH       int
		extraChrome int
		want        int
	}{
		// Below floor → OverlayMinH
		{"zero height", 0, 0, OverlayMinH},
		{"negative height", -1, 0, OverlayMinH},
		{"just below min body", OverlayChromeLines + OverlayMinH - 1, 0, OverlayMinH},
		// Exactly at floor
		{"exactly at min body", OverlayChromeLines + OverlayMinH, 0, OverlayMinH},
		// Above floor
		{"generous height", OverlayChromeLines + 10, 0, 10},
		// Extra chrome consumed
		{"with extra chrome", OverlayChromeLines + 10, 3, 7},
		// Extra chrome pushes below floor
		{"extra chrome forces min", OverlayChromeLines + OverlayMinH, 5, OverlayMinH},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OverlayBodyHeight(tt.termH, tt.extraChrome)
			if got != tt.want {
				t.Errorf("OverlayBodyHeight(%d, %d) = %d, want %d",
					tt.termH, tt.extraChrome, got, tt.want)
			}
		})
	}
}
