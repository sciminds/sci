package cmdutil

// navigate.go — helpers for enforcing the "namespace parents reject unknown
// children" invariant across the entire sci command tree.
//
// The problem: a command that exists purely to group subcommands (e.g.
// `sci zot item`) has no Action of its own. When the user types
// `sci zot item bogus`, urfave/cli v3's default behavior is either
//
//   - print "No help topic for 'bogus'" and exit 3 (for pure namespaces), or
//   - silently run the parent's Action with "bogus" as a positional arg
//     (for mixed Action+Commands shapes like `sci zot doctor`).
//
// Neither is acceptable. We want a consistent, scripted response: a
// [CodeUsage] error reading "unknown command %q. Did you mean %q?" — the same
// shape UsageErrorf uses for the empty-args case, down to the exit code and
// the rule that the help dump is suppressed under --json.
//
// Why not urfave's CommandNotFound? Two hard limits:
//
//  1. CommandNotFoundFunc has no error return — signaling a failure requires
//     cli.Exit(msg, code), which calls OsExiter (os.Exit by default) and
//     bypasses our styled root error handler.
//  2. It only fires inside urfave's default helpCommandAction, which is only
//     set when Action is nil. Mixed Action+Commands shapes never trigger it.
//
// The Before approach handles both cases: it returns an error that flows
// through the root handler's existing formatting, and it fires for every
// command shape uniformly.

import (
	"context"
	"maps"
	"sync"

	"github.com/urfave/cli/v3"
)

// wireMarker is the key under cmd.Metadata that WireNamespaceDefaults uses
// to track whether a command has already been wired, so repeated walks are
// idempotent.
const wireMarker = "cmdutil.namespaceDefaultsWired"

// deprecatedChildrenKey is the metadata key under which a namespace stores
// a map of removed → replacement subcommand names. RejectUnknownSubcommand
// consults this first; a match produces a tailored "removed — use X
// instead" error instead of the generic Levenshtein suggestion (which is
// useless when the user typed a name from muscle memory of the old API).
const deprecatedChildrenKey = "cmdutil.deprecatedChildren"

// MarkDeprecatedChildren records that the given subcommand names have been
// removed, with each value naming the recommended replacement. Typing one
// of the keys at this namespace produces a tailored error pointing at the
// new command. Idempotent: repeated calls merge their maps.
func MarkDeprecatedChildren(cmd *cli.Command, redirects map[string]string) {
	if cmd == nil || len(redirects) == 0 {
		return
	}
	if cmd.Metadata == nil {
		cmd.Metadata = map[string]any{}
	}
	merged, _ := cmd.Metadata[deprecatedChildrenKey].(map[string]string)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, redirects)
	cmd.Metadata[deprecatedChildrenKey] = merged
}

// RejectUnknownSubcommand is a Before hook for commands that group
// subcommands. If the first positional arg doesn't name a direct subcommand,
// it returns the shared [unknownSubcommandError] — a [CodeUsage] error naming
// the token, with a suggestion when one is worth making.
//
// No-op when:
//   - the first arg is empty (the namespace's Action handles it: pure
//     namespaces run [namespaceAction], mixed Action+Commands shapes like
//     `sci tools` run their own), or
//   - the first arg matches a real subcommand (descent continues normally).
func RejectUnknownSubcommand(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	first := cmd.Args().First()
	if first == "" || cmd.Command(first) != nil {
		return ctx, nil
	}
	if redirects, ok := cmd.Metadata[deprecatedChildrenKey].(map[string]string); ok {
		if replacement, ok := redirects[first]; ok {
			// Skip the help dump on a known-deprecated name: the tailored
			// "use X instead" line is already the answer the user needs.
			return ctx, Coded(CodeUsage, "%q was removed — use %q instead",
				cmd.FullName()+" "+first,
				cmd.FullName()+" "+replacement).
				WithFix(cmd.FullName() + " " + replacement)
		}
	}
	return ctx, unknownSubcommandError(cmd, first)
}

// ChainBefore composes multiple Before hooks into one, running each in order.
// Any hook returning a non-nil error short-circuits the chain. Each hook may
// return a derived context that's threaded into the next. nil entries are
// skipped, so it's safe to pass a command's existing (possibly nil) Before
// as the tail.
func ChainBefore(fns ...cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			next, err := fn(ctx, cmd)
			if err != nil {
				return ctx, err
			}
			if next != nil {
				ctx = next
			}
		}
		return ctx, nil
	}
}

// WireNamespaceDefaults walks the command tree rooted at root and installs
// the four behaviors sci wants everywhere, none of which urfave/cli gives us
// by default. Idempotent: a marker on cmd.Metadata prevents double-wiring if
// the tree is walked more than once.
//
// Per command:
//
//   - OnUsageError (every command) — a malformed flag becomes a [CodeUsage]
//     [CodedError] instead of urfave's duplicated "Incorrect Usage:" banner
//     with no code and exit 1. See [flagUsageError].
//
// Per namespace (a command that has subcommands):
//
//   - Before — [RejectUnknownSubcommand], so `sci zot bogus` is an error and
//     not urfave's "No help topic" or a silent fall-through to the parent.
//   - Action, when the namespace declares none — [namespaceAction], so a bare
//     `sci zot --json` refuses with an envelope instead of printing help and
//     exiting 0. A namespace with its own Action (`sci setup`, `sci zot
//     doctor`) already handles --json and is left alone.
//
// Once per process:
//
//   - the [cli.ShowCommandHelp] wrapper, so `sci <unknown> --help` reports
//     the same unknown-command error as `sci <unknown>`.
//
// Call once after the tree is fully assembled — e.g. in buildRoot() after
// SetupHelp. Every current and future command inherits all of it without
// per-site declarations.
func WireNamespaceDefaults(root *cli.Command) {
	if root == nil {
		return
	}
	installUnknownHelpTopicHandler()
	wireCommandDefaults(root)
}

// wireCommandDefaults is WireNamespaceDefaults' recursive half, split out so
// the exported function's one-time global install can't run per node.
func wireCommandDefaults(cmd *cli.Command) {
	if cmd.Metadata == nil {
		cmd.Metadata = map[string]any{}
	}
	if wired, _ := cmd.Metadata[wireMarker].(bool); !wired {
		if cmd.OnUsageError == nil {
			cmd.OnUsageError = onUsageError
		}
		if len(cmd.Commands) > 0 {
			cmd.Before = ChainBefore(RejectUnknownSubcommand, cmd.Before)
			if cmd.Action == nil {
				cmd.Action = namespaceAction
			}
		}
		cmd.Metadata[wireMarker] = true
	}
	for _, sub := range cmd.Commands {
		wireCommandDefaults(sub)
	}
}

// unknownHelpTopicOnce guards the one-time [cli.ShowCommandHelp] swap.
var unknownHelpTopicOnce sync.Once

// installUnknownHelpTopicHandler makes `sci <unknown> --help` fail the same
// way `sci <unknown>` does.
//
// urfave routes --help through its help-topic lookup, which on a miss builds
// "No help topic for 'convert'. cloud" — the trailing bare word is
// cli.SuggestCommand's return value concatenated with no template — and exits
// 3. Adding --help to a mistyped command should not change what sci tells you
// about the typo, so the miss is rerouted to [unknownSubcommandError].
//
// It has to be a swap of the package-level cli.ShowCommandHelp (the same
// technique SetupHelp uses for the help printer) because urfave's own hook
// for this, CommandNotFound, has no error return: signalling failure from
// there requires cli.Exit, which calls os.Exit and bypasses main's error
// handler entirely.
func installUnknownHelpTopicHandler() {
	unknownHelpTopicOnce.Do(func() {
		inner := cli.ShowCommandHelp
		cli.ShowCommandHelp = func(ctx context.Context, cmd *cli.Command, name string) error {
			if cmd.Command(name) != nil {
				return inner(ctx, cmd, name)
			}
			return unknownSubcommandError(cmd, name)
		}
	})
}
