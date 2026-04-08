package main

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/py/convert"
	"github.com/urfave/cli/v3"
)

func convertCommand() *cli.Command {
	return &cli.Command{
		Name:        "convert",
		Usage:       "Convert between marimo (.py), MyST (.md), and Quarto (.qmd)",
		Description: "$ sci py convert analysis.py report.qmd\n$ sci py convert notes.md notebook.py",
		ArgsUsage:   "<input> <output>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 2 {
				return cmdutil.UsageErrorf(cmd, "expected 2 arguments, got %d", cmd.Args().Len())
			}
			result, err := convert.Convert(cmd.Args().Get(0), cmd.Args().Get(1))
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, *result)
			return nil
		},
	}
}
