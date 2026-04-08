package main

import (
	"context"

	"github.com/sciminds/cli/internal/guide"
	"github.com/urfave/cli/v3"
)

func guideCommand() *cli.Command {
	return &cli.Command{
		Name:     "guide",
		Usage:    "Learn basic terminal commands with interactive demos",
		Category: "Getting Started",
		Aliases:  []string{"g"},
		Action: func(_ context.Context, _ *cli.Command) error {
			return guide.Run()
		},
	}
}
