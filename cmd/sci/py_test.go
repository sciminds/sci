package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestPyTutorials_JSONRequiresNameOrAll(t *testing.T) {
	ui.SetQuiet(false)
	root := buildRoot()

	// --json with no --name or --all should error instead of launching picker.
	err := root.Run(context.Background(), []string{"sci", "--json", "py", "tutorials"})

	if err == nil {
		t.Fatal("expected error when --json is set without --name or --all")
	}
	if !strings.Contains(err.Error(), "--name") || !strings.Contains(err.Error(), "--all") {
		t.Errorf("error should mention --name and --all, got: %v", err)
	}
	ui.SetQuiet(false)
}
