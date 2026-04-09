package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestCloudShare_JSONRequiresName(t *testing.T) {
	ui.SetQuiet(false)
	root := buildRoot()

	// Run cloud share with --json but no --name — should error.
	err := root.Run(context.Background(), []string{"sci", "--json", "cloud", "share", "somefile.csv"})

	if err == nil {
		t.Fatal("expected error when --json is set without --name")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("error should mention --name, got: %v", err)
	}
	ui.SetQuiet(false)
}
