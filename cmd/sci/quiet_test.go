package main

import (
	"context"
	"testing"

	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

func TestJSONFlag_SetsQuiet(t *testing.T) {
	ui.SetQuiet(false)
	t.Cleanup(func() { ui.SetQuiet(false) })

	var observed bool
	root := buildRoot()
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
}

func TestNoJSONFlag_QuietFalse(t *testing.T) {
	ui.SetQuiet(true)
	t.Cleanup(func() { ui.SetQuiet(false) })

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
