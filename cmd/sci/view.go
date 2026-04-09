package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/sciminds/cli/internal/mdview"
	"github.com/urfave/cli/v3"
)

func viewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Interactively browse data files or markdown documents",
		Description: "$ sci view data.csv\n$ sci view results.json\n$ sci view experiment.db\n$ sci view notes.md\n$ sci view docs/",
		ArgsUsage:   "<file|dir>",
		Category:    "Commands",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file or directory argument")
			}
			path := cmd.Args().First()
			if isMarkdown(path) {
				return mdview.Run(path)
			}
			return db.RunTUI(path, "")
		},
	}
}

// isMarkdown returns true for .md files or directories.
func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".md" || ext == ".markdown" {
		return true
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
