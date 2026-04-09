package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestDoctorReccs_JSONSkipsForm(t *testing.T) {
	ui.SetQuiet(false)
	root := buildRoot()

	// --json mode should not launch the interactive multi-select.
	// It will fail trying to access brew but should NOT fail on huh form.
	err := root.Run(context.Background(), []string{"sci", "--json", "doctor", "reccs"})

	// We expect either success or a brew-related error, NOT a TTY/huh error.
	if err != nil && strings.Contains(err.Error(), "TTY") {
		t.Errorf("--json mode should not open a TTY, got: %v", err)
	}
	ui.SetQuiet(false)
}
