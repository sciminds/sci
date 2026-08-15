package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/help"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// helpIsInteractive reports whether there is a terminal for the help browser
// to draw on and read from. Both ends matter: a piped stdout has nowhere to
// paint the alt screen, and a piped stdin has no keys to steer it with. Tests
// override.
var helpIsInteractive = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// helpScriptableTry names what a caller who cannot use the browser should run
// instead. It rides in Try (prose) rather than Fix: no rewrite of `sci help`
// makes an interactive picker scriptable, and Fix promises a command line that
// can be resubmitted verbatim.
const helpScriptableTry = "run `sci --help` for the command list, or `sci <command> --help` for one command's flags and examples"

func helpCommand() *cli.Command {
	return &cli.Command{
		Name:  "help",
		Usage: "Learn what each sci command does",
		Description: "$ sci help\n" +
			"$ sci help cloud\n" +
			"$ sci help zot",
		Category:  "Getting Started",
		Aliases:   []string{"h"},
		ArgsUsage: "[command]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			// urfave/cli hands a command *named* "help" its raw argv tail
			// without parsing a single flag — command_run.go short-circuits on
			// `cmd.Name == helpName` and calls the Action directly — so
			// --help, --json and a mistyped topic all arrive here as
			// positional arguments. This Action is therefore the only place
			// that can tell them apart; before it did, `sci help --help`
			// answered "unknown command: --help" and exited 1.
			args := cmd.Args().Slice()

			if lo.Some(args, []string{"--help", "-h"}) {
				return cli.ShowSubcommandHelp(cmd)
			}
			if cmdutil.JSONRequested(args) {
				return cmdutil.Coded(cmdutil.CodeUsage, "help is interactive-only and has no --json mode").
					WithTry(helpScriptableTry)
			}
			if flag, found := lo.Find(args, func(a string) bool {
				return len(a) > 1 && strings.HasPrefix(a, "-")
			}); found {
				return cmdutil.Coded(cmdutil.CodeUsage, "unknown flag %q for 'sci help'", flag).
					WithTry(helpScriptableTry)
			}

			groups := help.BuildGroups(cmd.Root())

			// A named command jumps straight to that group. Resolve it before
			// the terminal check so a typo is reported as a typo even from a
			// pipe — that is a command line to rewrite, not a place to run.
			var group *help.CommandGroup
			if len(args) > 0 {
				if group = help.FindGroup(groups, args[0]); group == nil {
					return unknownHelpTopic(groups, args[0])
				}
			}

			if !helpIsInteractive() {
				return cmdutil.Coded(cmdutil.CodeUsage,
					"help is an interactive browser and needs a terminal (stdin and stdout are not a TTY)").
					WithTry(helpScriptableTry)
			}

			if group != nil {
				return help.RunGroup(group)
			}
			return help.Run(groups)
		},
	}
}

// unknownHelpTopic is the answer to `sci help nosuchthing`: a usage error
// (exit 2 — rewrite the command line) naming what was typed and what exists,
// rather than the exit-1 "unknown command" this used to return.
func unknownHelpTopic(groups []help.CommandGroup, typed string) error {
	names := lo.Map(groups, func(g help.CommandGroup, _ int) string { return g.Name })
	slices.Sort(names)

	err := cmdutil.Coded(cmdutil.CodeUsage, "unknown help topic %q", typed)
	if s := suggestTopic(names, typed); s != "" {
		return err.WithTry(fmt.Sprintf("did you mean %q? Otherwise pick one of: %s",
			s, strings.Join(names, ", ")))
	}
	return err.WithTry("pick one of: " + strings.Join(names, ", "))
}

// suggestTopic returns the topic a mistyped name most plausibly meant, or "".
//
// Deliberately a prefix/substring test rather than an edit distance: urfave's
// own Jaro-Winkler suggester is unexported, and topics are short command names
// where the miss that actually happens is a truncation (`sci help zo`).
func suggestTopic(names []string, typed string) string {
	t := strings.ToLower(typed)
	match, ok := lo.Find(names, func(n string) bool {
		return strings.HasPrefix(n, t) || strings.HasPrefix(t, n) || strings.Contains(n, t)
	})
	if !ok {
		return ""
	}
	return match
}
