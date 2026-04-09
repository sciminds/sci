package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestProjNew_JSONRequiresNameArg(t *testing.T) {
	ui.SetQuiet(false)
	root := buildRoot()

	// --json with no name argument should error instead of launching wizard.
	err := root.Run(context.Background(), []string{"sci", "--json", "proj", "new"})

	if err == nil {
		t.Fatal("expected error when --json is set without a name argument")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention project name, got: %v", err)
	}
	ui.SetQuiet(false)
}
