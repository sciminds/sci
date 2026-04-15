package main

import (
	"context"
	"os"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/urfave/cli/v3"
)

func viewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Interactively browse tabular data files (CSV, JSON, SQLite)",
		Description: "$ sci view data.csv\n$ sci view results.json\n$ sci view experiment.db",
		ArgsUsage:   "<file>",
		Category:    "Commands",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file argument")
			}
			path := cmd.Args().First()

			info, err := os.Stat(path)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s: %v", path, err)
			}
			if info.IsDir() {
				return cmdutil.UsageErrorf(cmd, "%s is a directory — sci view expects a tabular data file (CSV, JSON, SQLite)", path)
			}

			return db.RunTUI(path, "")
		},
	}
}
