package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
)

func TestCloudPut_JSONRequiresName(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	root := buildRoot()

	// Run cloud put with --json but no --name — should error.
	err := root.Run(context.Background(), []string{"sci", "--json", "cloud", "put", "somefile.csv"})

	if err == nil {
		t.Fatal("expected error when --json is set without --name")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("error should mention --name, got: %v", err)
	}
}
