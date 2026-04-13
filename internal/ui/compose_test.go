package ui

import "testing"

// The full test suite lives in internal/tui/compose/compose_test.go.
// This file verifies the re-exports compile and delegate correctly.

func TestSpread_reexport(t *testing.T) {
	t.Parallel()
	got := Spread(20, "L", "R")
	if got == "" {
		t.Error("Spread re-export returned empty")
	}
}

func TestFit_reexport(t *testing.T) {
	t.Parallel()
	got := Fit("hello world", 5, 0)
	if got == "" {
		t.Error("Fit re-export returned empty")
	}
}
