package main

import (
	"context"
	"testing"

	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

func TestJSONFlag_SetsQuiet(t *testing.T) {
	ui.SetQuiet(false) // reset

	var observed bool
	root := buildRoot()
	// Add a tiny subcommand that captures the quiet state after Before runs.
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = ui.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "--json", "test-quiet"})

	if !observed {
		t.Error("--json should set ui.IsQuiet() to true")
	}
	ui.SetQuiet(false) // cleanup
}

func TestNoJSONFlag_QuietFalse(t *testing.T) {
	ui.SetQuiet(true) // start with true to verify it gets set to false

	var observed bool
	root := buildRoot()
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = ui.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "test-quiet"})

	if observed {
		t.Error("without --json, ui.IsQuiet() should be false")
	}
}
