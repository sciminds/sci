package main

import (
	"context"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

func TestJSONFlag_SetsQuiet(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	var observed bool
	root := buildRoot()
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = uikit.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "--json", "test-quiet"})

	if !observed {
		t.Error("--json should set uikit.IsQuiet() to true")
	}
}

func TestNoJSONFlag_QuietFalse(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	var observed bool
	root := buildRoot()
	root.Commands = append(root.Commands, &cli.Command{
		Name: "test-quiet",
		Action: func(_ context.Context, _ *cli.Command) error {
			observed = uikit.IsQuiet()
			return nil
		},
	})

	_ = root.Run(context.Background(), []string{"sci", "test-quiet"})

	if observed {
		t.Error("without --json, uikit.IsQuiet() should be false")
	}
}
