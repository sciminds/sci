package main

import (
	"context"

	"github.com/sciminds/cli/internal/guide"
	"github.com/urfave/cli/v3"
)

func learnCommand() *cli.Command {
	return &cli.Command{
		Name:     "learn",
		Usage:    "Learn the command-line, Python, & more with interactive demos!",
		Category: "Getting Started",
		Aliases:  []string{"l"},
		Action: func(_ context.Context, _ *cli.Command) error {
			return guide.Run(guide.Books)
		},
	}
}
