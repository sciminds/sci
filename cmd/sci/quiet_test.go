package main

import (
	"context"
	"testing"

	"github.com/sciminds/cli/internal/cliui"
	"github.com/urfave/cli/v3"
)

func TestJSONFlag_SetsQuiet(t *testing.T) {
	cliui.SetQuiet(false)
	t.Cleanup(func() { cliui.SetQuiet(false) })

	var observed bool
	root := buildRoot()
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = cliui.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "--json", "test-quiet"})

	if !observed {
		t.Error("--json should set cliui.IsQuiet() to true")
	}
}

func TestNoJSONFlag_QuietFalse(t *testing.T) {
	cliui.SetQuiet(true)
	t.Cleanup(func() { cliui.SetQuiet(false) })

	var observed bool
	root := buildRoot()
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = cliui.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "test-quiet"})

	if observed {
		t.Error("without --json, cliui.IsQuiet() should be false")
	}
}
