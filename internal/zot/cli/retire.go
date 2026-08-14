package cli

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"
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
// Both shapes route their refusal through [stubAction], which is what
// keeps --json and --help working on a command that parses no flags.
//
// The per-verb constructors are in moved.go, where the whole boundary
// reads as one list.

// stubAction wraps a stub's refusal in the two behaviours SkipFlagParsing
// costs it. SkipFlagParsing itself is load-bearing — it is what lets a
// script still passing the old write flags reach the explanation instead
// of a bare "flag provided but not defined" — but urfave hands those flags
// to the Action as positionals and parses none of them, so the two flags
// that are not the verb's own arrive inert:
//
//   - --json. `sci zot item add --json` left stdout EMPTY and rendered the
//     refusal as human text on stderr, because [cmdutil.IsJSON] reads the
//     parsed flag. Trailing --json is the position the guide and every help
//     screen teach, so an agent hitting a retired verb got a JSON parse
//     error on empty input instead of the Try telling it where the verb
//     went. The stub therefore reads its own positionals and re-arms the
//     root's flag, which keeps both positions byte-identical: main() still
//     renders through the one failure sink, [cmdutil.HandleError], off the
//     same flag value.
//   - --help. It printed the one-line error, leaving the authored
//     Description — the clearest statement of the boundary in the tree —
//     reachable only through the `sci help` TUI, which a scripting user
//     never opens.
//
// --json wins over --help, the same discipline [cmdutil.UsageErrorf]
// applies: the consumer under --json is a machine, and a styled help page
// on its stdout is not the envelope it is parsing.
func stubAction(refuse func() error) func(context.Context, *cli.Command) error {
	return func(_ context.Context, cmd *cli.Command) error {
		swallowed := cmd.Args().Slice()
		jsonMode := cmdutil.IsJSON(cmd)
		if swallowedJSON(swallowed) {
			jsonMode = true
			// Best-effort: a root that declares no --json (a test harness
			// mounting the tree on its own) simply keeps the human branch.
			_ = cmd.Set("json", "true")
		}
		if !jsonMode && swallowedHelp(swallowed) {
			return cli.ShowSubcommandHelp(cmd)
		}
		return refuse()
	}
}

// beforeDoubleDash truncates a stub's swallowed positionals at a bare
// `--`. Everything after it is a literal argument in urfave's own parsing,
// so a stub must not read `--json` or `--help` there either: `sci zot llm
// query -s x -- --json` asked to pass the string through, and mirroring
// real flag parsing is what makes the detection below trustworthy.
func beforeDoubleDash(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[:i]
	}
	return args
}

// swallowedJSON reports whether --json survives among the tokens
// SkipFlagParsing handed the stub. It accepts every spelling urfave would
// have parsed — bare `--json`, single-dash `-json`, and the `=` form with
// any value strconv.ParseBool understands — and lets the last occurrence
// win, so `--json=false` disables it exactly as it would on a live verb.
func swallowedJSON(args []string) bool {
	found := false
	for _, tok := range beforeDoubleDash(args) {
		name, value, hasValue := strings.Cut(tok, "=")
		if name != "--json" && name != "-json" {
			continue
		}
		if !hasValue {
			found = true
			continue
		}
		if b, err := strconv.ParseBool(value); err == nil {
			found = b
		}
	}
	return found
}

// swallowedHelp reports whether a help flag survives among the tokens
// SkipFlagParsing handed the stub. Unlike `sci proj run`, position does
// not matter here: a stub has no task to forward flags to, so every token
// on the line belongs to the retired verb.
func swallowedHelp(args []string) bool {
	return lo.ContainsBy(beforeDoubleDash(args), func(tok string) bool {
		return slices.Contains([]string{"--help", "-help", "-h"}, tok)
	})
}

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
//
// The Description below is the long form of the boundary, and [stubAction]
// is what makes `sci zot item add --help` print it — write it for someone
// reading it at the point of failure, not just in the `sci help` TUI.
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
		Action: stubAction(func() error {
			return movedToZotError(oldPath, zotCmd, why)
		}),
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
		Action: stubAction(func() error {
			return retiredOutrightError(oldPath, why, remedy)
		}),
	}
}
