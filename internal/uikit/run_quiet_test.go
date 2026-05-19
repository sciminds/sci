package uikit

import "testing"

// These tests mutate the package-global `quiet` and intentionally do NOT
// call t.Parallel — running them concurrently would race on the global
// under `go test -race`.

func TestQuiet_DefaultFalse(t *testing.T) {
	SetQuiet(false)
	if IsQuiet() {
		t.Error("IsQuiet should be false by default")
	}
}

func TestQuiet_SetTrue(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)
	if !IsQuiet() {
		t.Error("IsQuiet should be true after SetQuiet(true)")
	}
}

func TestQuiet_SetFalseRestores(t *testing.T) {
	SetQuiet(true)
	SetQuiet(false)
	if IsQuiet() {
		t.Error("IsQuiet should be false after SetQuiet(false)")
	}
}
