package uikit

import "testing"

func TestQuiet_DefaultFalse(t *testing.T) {
	t.Parallel()
	// Reset to known state.
	SetQuiet(false)
	if IsQuiet() {
		t.Error("IsQuiet should be false by default")
	}
}

func TestQuiet_SetTrue(t *testing.T) {
	t.Parallel()
	SetQuiet(true)
	defer SetQuiet(false)
	if !IsQuiet() {
		t.Error("IsQuiet should be true after SetQuiet(true)")
	}
}

func TestQuiet_SetFalseRestores(t *testing.T) {
	t.Parallel()
	SetQuiet(true)
	SetQuiet(false)
	if IsQuiet() {
		t.Error("IsQuiet should be false after SetQuiet(false)")
	}
}
