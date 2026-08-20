//go:build linux

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/uikit"
)

// TestDoctor_Linux_NoBrewReferences asserts the Linux doctor never mentions
// Homebrew or Brewfile — both are macOS-only on this branch. This guards
// against accidentally falling back to a brew-flavoured code path on Linux.
func TestDoctor_Linux_NoBrewReferences(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	// Capture stderr too because the human-mode doctor prints progress there.
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			root := buildRoot()
			// We expect this to exit cleanly or surface a check failure; either
			// way the output must not reference brew/Brewfile on Linux.
			_ = root.Run(context.Background(), []string{"sci", "doctor", "--json"})
		})
	})
	combined := stderr + stdout

	if strings.Contains(strings.ToLower(combined), "homebrew") {
		t.Errorf("Linux doctor output mentions Homebrew:\n%s", combined)
	}
	if strings.Contains(combined, "Brewfile") {
		t.Errorf("Linux doctor output mentions Brewfile:\n%s", combined)
	}
}
