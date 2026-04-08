package main

import (
	"context"

	"github.com/sciminds/cli/internal/guide"
	"github.com/urfave/cli/v3"
)

func guideCommand() *cli.Command {
	return &cli.Command{
		Name:     "guide",
		Usage:    "Interactive guides with terminal demos",
		Category: "Getting Started",
		Aliases:  []string{"g"},
		Commands: []*cli.Command{
			{
				Name:    "basic",
				Usage:   "Learn basic terminal commands (ls, cd, cp, mv, …)",
				Aliases: []string{"b"},
				Action: func(_ context.Context, _ *cli.Command) error {
					return guide.Run("Terminal Guide", guide.BasicEntries)
				},
			},
			{
				Name:    "git",
				Usage:   "Learn essential Git commands (init, add, commit, push, …)",
				Aliases: []string{"g"},
				Action: func(_ context.Context, _ *cli.Command) error {
					return guide.Run("Git Guide", guide.GitEntries)
				},
			},
		},
	}
}
