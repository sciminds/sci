//go:build linux

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
)

// TestDoctor_Linux_NoBrewReferences asserts the Linux doctor never mentions
// Homebrew or Brewfile — both are macOS-only on this branch. This guards
// against accidentally falling back to a brew-flavoured code path on Linux.
func TestDoctor_Linux_NoBrewReferences(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	// Capture stderr because the human-mode doctor prints progress there.
	oldErr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	oldOut := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	root := buildRoot()
	// We expect this to exit cleanly or surface a check failure; either way
	// the output must not reference brew/Brewfile on Linux.
	_ = root.Run(context.Background(), []string{"sci", "doctor", "--json"})

	_ = wErr.Close()
	_ = wOut.Close()
	os.Stderr = oldErr
	os.Stdout = oldOut

	var stderr, stdout bytes.Buffer
	_, _ = io.Copy(&stderr, rErr)
	_, _ = io.Copy(&stdout, rOut)
	combined := stderr.String() + stdout.String()

	if strings.Contains(strings.ToLower(combined), "homebrew") {
		t.Errorf("Linux doctor output mentions Homebrew:\n%s", combined)
	}
	if strings.Contains(combined, "Brewfile") {
		t.Errorf("Linux doctor output mentions Brewfile:\n%s", combined)
	}
}
