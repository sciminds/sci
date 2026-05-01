package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

func viewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Interactively browse data files (CSV, JSON, SQLite) or markdown documents",
		Description: "$ sci view data.csv\n$ sci view results.json\n$ sci view experiment.db\n$ sci view notes.md",
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
				return cmdutil.UsageErrorf(cmd, "%s is a directory — sci view expects a file. For multi-page collections, see `sci learn`.", path)
			}

			if isMarkdown(path) {
				return uikit.RunMdViewer(path)
			}

			return db.RunTUI(path, "")
		},
	}
}

// isMarkdown returns true for paths whose extension matches a markdown
// document (.md or .markdown, case-insensitive).
func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}
