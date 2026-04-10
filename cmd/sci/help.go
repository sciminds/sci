package main

import (
	"context"

	"github.com/sciminds/cli/internal/helptui"
	"github.com/urfave/cli/v3"
)

func helpCommand() *cli.Command {
	return &cli.Command{
		Name:      "help",
		Usage:     "Get-to-know what each sci command(s) does",
		Category:  "What Can I Do?",
		Aliases:   []string{"h"},
		ArgsUsage: "[command]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			groups := helptui.BuildGroups(cmd.Root())

			// If a command name was given, jump straight to that group.
			if cmd.Args().Present() {
				name := cmd.Args().First()
				g := helptui.FindGroup(groups, name)
				if g == nil {
					return cli.Exit("unknown command: "+name, 1)
				}
				return helptui.RunGroup(g)
			}

			return helptui.Run(groups)
		},
	}
}
