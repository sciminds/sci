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
// Neither is acceptable. We want a consistent, scripted response: dump help,
// print "unknown command %q. Did you mean %q?", and exit 1 — the same shape
// UsageErrorf uses for the empty-args case.
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
	"fmt"

	"github.com/urfave/cli/v3"
)

// wireMarker is the key under cmd.Metadata that WireNamespaceDefaults uses
// to track whether a command has already been wired, so repeated walks are
// idempotent.
const wireMarker = "cmdutil.namespaceDefaultsWired"

// RejectUnknownSubcommand is a Before hook for commands that group
// subcommands. If the first positional arg doesn't name a direct subcommand,
// it dumps the parent's help and returns an error.
//
// No-op when:
//   - the first arg is empty (urfave will auto-show help via the default
//     Action — the empty-args case is fine), or
//   - the first arg matches a real subcommand (descent continues normally).
func RejectUnknownSubcommand(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	first := cmd.Args().First()
	if first == "" || cmd.Command(first) != nil {
		return ctx, nil
	}
	_ = cli.ShowSubcommandHelp(cmd)
	msg := fmt.Sprintf("unknown command %q", first)
	if s := cli.SuggestCommand(cmd.Commands, first); s != "" {
		msg += fmt.Sprintf(". Did you mean %q?", s)
	}
	return ctx, fmt.Errorf("%s", msg)
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

// WireNamespaceDefaults walks the command tree rooted at root and chains
// RejectUnknownSubcommand into the Before of every command that has
// subcommands. Idempotent: a marker on cmd.Metadata prevents double-wiring
// if the tree is walked more than once.
//
// Call once after the tree is fully assembled — e.g. in buildRoot() after
// SetupHelp. Every current and future namespace inherits the invariant
// without per-site declarations.
func WireNamespaceDefaults(root *cli.Command) {
	if root == nil {
		return
	}
	if len(root.Commands) > 0 {
		if root.Metadata == nil {
			root.Metadata = map[string]any{}
		}
		if wired, _ := root.Metadata[wireMarker].(bool); !wired {
			root.Before = ChainBefore(RejectUnknownSubcommand, root.Before)
			root.Metadata[wireMarker] = true
		}
	}
	for _, sub := range root.Commands {
		WireNamespaceDefaults(sub)
	}
}
