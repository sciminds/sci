package main

import (
	"strings"
	"testing"
)

// TestZotCommandPointsToGuide is the help-pointer drift fence: the `sci zot`
// description must mention `sci zot guide` so an agent inspecting --help
// discovers the cheat sheet.
func TestZotCommandPointsToGuide(t *testing.T) {
	zot := zotCommand()
	if !strings.Contains(zot.Description, "guide") {
		t.Errorf("zot Description should mention `sci zot guide` so --help points to the cheat sheet:\n%s", zot.Description)
	}
}
