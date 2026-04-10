package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestLabSetup_JSONRequiresUser(t *testing.T) {
	ui.SetQuiet(false)
	t.Cleanup(func() { ui.SetQuiet(false) })

	root := buildRoot()

	err := root.Run(context.Background(), []string{"sci", "--json", "lab", "setup"})

	if err == nil {
		t.Fatal("expected error when --json is set without --user")
	}
	if !strings.Contains(err.Error(), "--user") {
		t.Errorf("error should mention --user, got: %v", err)
	}
}

func TestLabSetup_HasUserFlag(t *testing.T) {
	root := buildRoot()
	lab := findCmd(root.Commands, "lab")
	if lab == nil {
		t.Fatal("lab command not found")
	}
	setup := findCmd(lab.Commands, "setup")
	if setup == nil {
		t.Fatal("lab setup not found")
	}
	if !hasFlag(setup, "user") {
		t.Error("lab setup should have a --user flag")
	}
}
