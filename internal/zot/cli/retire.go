package cli

import (
	"context"
	"strings"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/urfave/cli/v3"
)

// This file holds the two shapes a retirement can take, and the difference
// between them is where the work went:
//
//   - [movedToZotCommand] — the verb lives on in the sibling `zot` binary.
//   - [retiredOutrightCommand] — the verb has no home in any binary, and
//     the remedy is prose (an app to open, a habit to change).
//
// Neither ever fills Fix. A moved verb cannot, because `zot` is a different
// program and is absent from lab machines; a retired one cannot, because
// there is no command to hand back at all. Error.Fix is verbatim-runnable
// or absent, and an agent will run whatever lands there.
//
// The per-verb constructors are in moved.go, where the whole boundary
// reads as one list.

// movedToZotError reports that a verb now lives in the separate `zot`
// binary rather than anywhere in sci.
func movedToZotError(oldPath []string, zotCmd, why string) error {
	return cmdutil.Coded(cmdutil.CodeUsage,
		"`zot %s` has been retired from sci — sci's Zotero surface is local and read-only",
		strings.Join(oldPath, " ")).
		WithTry(why + "; on a machine that has zot installed, run `" + zotCmd + "`")
}

// movedToZotCommand builds a stub for a verb that left sci for the `zot`
// binary. Keeping the name registered is deliberate: urfave would
// otherwise answer `zot item add` with a bare "command not found", which
// tells the user nothing about where to go next. SkipFlagParsing so a script
// still passing the old flags reaches the explanation instead of an
// unknown-flag error.
//
// A whole namespace retires as ONE leaf stub rather than a stub per verb:
// with SkipFlagParsing the subcommand name arrives as a positional
// argument, so `sci zot content read KEY` reaches this Action intact. Only
// namespaces that KEPT some verbs (item, collection) stub their children
// one at a time.
func movedToZotCommand(name, usage string, oldPath []string, zotCmd, why string) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Description: "Retired from sci. The equivalent verb is `" + zotCmd + "` in the zot binary.\n\n" +
			why + ".\n\n" +
			"sci's Zotero surface reads the local Zotero database and stops there:\n" +
			"it never writes to your library, fetches from an upstream index, or\n" +
			"spends a metered API call.",
		SkipFlagParsing: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			return movedToZotError(oldPath, zotCmd, why)
		},
	}
}

// retiredOutrightError reports that a verb is gone from every SciMinds
// binary, and hands back prose instead of a destination.
//
// remedy is a sentence, not a command line: it names what to do instead
// (open an app, change a habit) for the cases where nothing sci or zot can
// run is the right answer. Writing a plausible-looking `zot …` string here
// would be worse than saying nothing — an agent would run it and get
// "command not found" from a second tool.
func retiredOutrightError(oldPath []string, why, remedy string) error {
	return cmdutil.Coded(cmdutil.CodeUsage,
		"`zot %s` has been retired — %s",
		strings.Join(oldPath, " "), why).
		WithTry(remedy)
}

// retiredOutrightCommand builds a stub for a verb that left with no
// replacement anywhere. Registered and SkipFlagParsing for the same
// reasons as [movedToZotCommand]: a bare "command not found" teaches
// nothing, and a script still passing the old flags must reach the
// explanation rather than an unknown-flag error.
func retiredOutrightCommand(name, usage string, oldPath []string, why, remedy string) *cli.Command {
	return &cli.Command{
		Name:            name,
		Usage:           usage,
		Description:     "Retired from sci — " + why + ".\n\n" + remedy + ".",
		SkipFlagParsing: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			return retiredOutrightError(oldPath, why, remedy)
		},
	}
}
