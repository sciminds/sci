package uikit

import (
	"testing"
)

func TestResponsive_MatchesHighest(t *testing.T) {
	t.Parallel()
	// Width 100 should match the lg breakpoint (>= 80), not md (>= 60).
	got := Responsive(100, 24).
		When(80, func(w, h int) string { return "lg" }).
		When(60, func(w, h int) string { return "md" }).
		Default(func(w, h int) string { return "sm" }).
		Render()

	if got != "lg" {
		t.Errorf("got %q, want 'lg'", got)
	}
}

func TestResponsive_MatchesMid(t *testing.T) {
	t.Parallel()
	// Width 70 should match md (>= 60) but not lg (>= 80).
	got := Responsive(70, 24).
		When(80, func(w, h int) string { return "lg" }).
		When(60, func(w, h int) string { return "md" }).
		Default(func(w, h int) string { return "sm" }).
		Render()

	if got != "md" {
		t.Errorf("got %q, want 'md'", got)
	}
}

func TestResponsive_FallsToDefault(t *testing.T) {
	t.Parallel()
	// Width 40 doesn't match any breakpoint.
	got := Responsive(40, 24).
		When(80, func(w, h int) string { return "lg" }).
		When(60, func(w, h int) string { return "md" }).
		Default(func(w, h int) string { return "sm" }).
		Render()

	if got != "sm" {
		t.Errorf("got %q, want 'sm'", got)
	}
}

func TestResponsive_NoDefault(t *testing.T) {
	t.Parallel()
	// No default and no match → empty string.
	got := Responsive(40, 24).
		When(80, func(w, h int) string { return "lg" }).
		Render()

	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestResponsive_PassesDimensions(t *testing.T) {
	t.Parallel()
	// The matched callback should receive the actual width and height.
	var gotW, gotH int
	Responsive(120, 40).
		When(80, func(w, h int) string {
			gotW, gotH = w, h
			return ""
		}).
		Render()

	if gotW != 120 {
		t.Errorf("width = %d, want 120", gotW)
	}
	if gotH != 40 {
		t.Errorf("height = %d, want 40", gotH)
	}
}

func TestResponsive_ExactBreakpoint(t *testing.T) {
	t.Parallel()
	// Width exactly at breakpoint should match.
	got := Responsive(80, 24).
		When(80, func(w, h int) string { return "match" }).
		Default(func(w, h int) string { return "default" }).
		Render()

	if got != "match" {
		t.Errorf("got %q, want 'match'", got)
	}
}

func TestResponsive_InsertionOrder(t *testing.T) {
	t.Parallel()
	// Breakpoints should work regardless of insertion order.
	// Add smaller breakpoint first, larger second.
	got := Responsive(100, 24).
		When(60, func(w, h int) string { return "md" }).
		When(80, func(w, h int) string { return "lg" }).
		Default(func(w, h int) string { return "sm" }).
		Render()

	// Should still match lg (highest matching), not md.
	if got != "lg" {
		t.Errorf("got %q, want 'lg' (insertion order shouldn't matter)", got)
	}
}

func TestResponsive_DefaultOnly(t *testing.T) {
	t.Parallel()
	// Only a default, no breakpoints.
	got := Responsive(40, 24).
		Default(func(w, h int) string { return "only" }).
		Render()

	if got != "only" {
		t.Errorf("got %q, want 'only'", got)
	}
}
