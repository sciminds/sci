package main

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/urfave/cli/v3"
)

func viewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Interactively browse any tabular data file (csv, db, etc)",
		Description: "$ sci view data.csv\n$ sci view results.json\n$ sci view experiment.db",
		ArgsUsage:   "<file>",
		Category:    "Commands",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file argument")
			}
			return db.RunTUI(cmd.Args().First(), "")
		},
	}
}
