package main

import (
	"context"

	"github.com/sciminds/cli/internal/help"
	"github.com/urfave/cli/v3"
)

func helpCommand() *cli.Command {
	return &cli.Command{
		Name:  "help",
		Usage: "Get-to-know what each sci command(s) does",
		Description: "$ sci help\n" +
			"$ sci help cloud\n" +
			"$ sci help zot",
		Category:  "What Can I Do?",
		Aliases:   []string{"h"},
		ArgsUsage: "[command]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			groups := help.BuildGroups(cmd.Root())

			// If a command name was given, jump straight to that group.
			if cmd.Args().Present() {
				name := cmd.Args().First()
				g := help.FindGroup(groups, name)
				if g == nil {
					return cli.Exit("unknown command: "+name, 1)
				}
				return help.RunGroup(g)
			}

			return help.Run(groups)
		},
	}
}
