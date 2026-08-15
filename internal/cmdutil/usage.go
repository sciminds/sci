package cmdutil

// usage.go — translating urfave/cli's own failure modes into sci's error
// contract, so that "the command line is wrong" always looks the same
// whichever layer noticed it.
//
// urfave has three ways of rejecting a command line, and left to itself each
// one produces a different shape:
//
//   - flag parsing fails → it prints "Incorrect Usage: …" to stderr *and*
//     returns the error, so the message renders twice, and the error carries
//     no [Code], so the process exits 1 where every other misuse exits 2;
//   - a namespace parent runs with no subcommand → it prints its (styled)
//     help page and exits 0, which under --json hands an agent a success code
//     with unparseable output;
//   - `sci <unknown> --help` → it builds "No help topic for 'x'. y", raw
//     suggestion concatenated with no template, and exits 3.
//
// All three become the same [CodeUsage] [CodedError] here, which means one
// envelope shape, one exit code (2 = rewrite the command line and retry) and
// one place to improve the wording.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"
)

// JSONRequested reports whether --json appears in a raw argv slice.
//
// It exists for the one moment [IsJSON] cannot answer: when flag *parsing*
// itself failed, no flag was ever assigned, so the parsed --json is false
// even though the caller plainly asked for machine output. Without this, a
// typo'd flag under --json emits a human ✗ line and no envelope — the exact
// case an agent has no way to recover from.
//
// Deliberately literal: it matches the flag spelling, not a flag *value* that
// happens to be the string "--json". A command whose own argument is
// literally "--json" and which then fails would render its failure as JSON;
// that is a strange command line, and an envelope is a defensible answer to
// it either way.
func JSONRequested(args []string) bool {
	return lo.SomeBy(args, func(a string) bool {
		return a == "--json" || a == "-json" || a == "--json=true" || a == "-json=true"
	})
}

// invalidValueRe matches the standard library's flag-parse failure,
// "invalid value \"x\" for flag -name: <parser error>", so the parser error
// (which names Go internals) can be replaced with something a reader can act
// on. The value capture is greedy so an embedded quote stays inside it —
// " for flag -" is the anchor.
var invalidValueRe = regexp.MustCompile(`^invalid value "(.*)" for flag -(\S+): (.+)$`)

// undefinedFlagPrefix is the standard library's message for a flag the
// command never declared.
const undefinedFlagPrefix = "flag provided but not defined: -"

// flagUsageError converts a raw flag-parsing failure into a [CodedError].
//
// Deliberately no help dump: urfave's default prints the entire help page on
// a mistyped flag, which buries the one line that matters. The message names
// the flag, the try names the fix, and `--help` is one keystroke away.
func flagUsageError(cmd *cli.Command, err error) error {
	raw := err.Error()
	helpHint := fmt.Sprintf("run '%s --help' to see the available flags", cmd.FullName())

	if name, ok := strings.CutPrefix(raw, undefinedFlagPrefix); ok {
		coded := Coded(CodeUsage, "unknown flag %q for '%s'", "--"+name, cmd.FullName())
		if s := suggestFlagName(cmd, name); s != "" {
			return coded.WithTry(fmt.Sprintf("did you mean --%s? Otherwise %s", s, helpHint))
		}
		return coded.WithTry(helpHint)
	}

	if m := invalidValueRe.FindStringSubmatch(raw); m != nil {
		value, name, parseErr := m[1], m[2], m[3]
		if want := expectedValueKind(parseErr); want != "" {
			return Coded(CodeUsage, "--%s expects %s, got %q", name, want, value).
				WithTry(helpHint)
		}
		return Coded(CodeUsage, "invalid value %q for --%s", value, name).WithTry(helpHint)
	}

	// urfave's own mutual-exclusion message is a conflict, not a malformed
	// flag — the caller has to drop one of two flags, not fix a spelling.
	if strings.Contains(raw, "cannot be set along with option") {
		return Coded(CodeConflict, "%s", raw).WithTry(helpHint)
	}

	return Coded(CodeUsage, "%s", raw).WithTry(helpHint)
}

// expectedValueKind maps a Go parser error to the kind of value a reader
// should supply, or "" when we don't recognize it. "strconv.ParseInt:
// parsing \"notanumber\": invalid syntax" is true and useless; "expects a
// whole number" is what the reader needs.
func expectedValueKind(parseErr string) string {
	switch {
	case strings.Contains(parseErr, "ParseInt"), strings.Contains(parseErr, "ParseUint"):
		return "a whole number"
	case strings.Contains(parseErr, "ParseFloat"):
		return "a number"
	case strings.Contains(parseErr, "ParseBool"):
		return "true or false"
	case strings.Contains(parseErr, "invalid duration"):
		return "a duration like 30s or 5m"
	default:
		return ""
	}
}

// onUsageError is the [cli.OnUsageErrorFunc] wired onto every command in the
// tree by [WireNamespaceDefaults]. Returning an error from here also
// suppresses urfave's own "Incorrect Usage:" banner and help dump, which is
// what made the message print twice.
func onUsageError(_ context.Context, cmd *cli.Command, err error, _ bool) error {
	return flagUsageError(cmd, err)
}

// namespaceAction is the Action installed on every command that groups
// subcommands and has none of its own.
//
// Human mode keeps the behavior a person expects: print the help page, exit
// 0. Under --json it refuses instead, because a success exit code attached to
// a page of ANSI-styled prose is the one shape an agent cannot recover from —
// it can neither parse the output nor tell that anything went wrong. The
// refusal enumerates the subcommands so the next call can be right.
func namespaceAction(_ context.Context, cmd *cli.Command) error {
	if !IsJSON(cmd) {
		if cmd.Root() == cmd {
			return cli.ShowAppHelp(cmd)
		}
		return cli.ShowSubcommandHelp(cmd)
	}
	names := lo.FilterMap(cmd.Commands, func(sub *cli.Command, _ int) (string, bool) {
		return sub.Name, !sub.Hidden && sub.Name != "help"
	})
	return Coded(CodeUsage, "%q is a command group and has no output of its own", cmd.FullName()).
		WithTry(fmt.Sprintf("run '%s <subcommand> --json' — subcommands: %s",
			cmd.FullName(), strings.Join(names, ", ")))
}

// unknownSubcommandError is the single answer to "you named a child that
// doesn't exist", shared by the plain path ([RejectUnknownSubcommand]) and
// the --help path ([installUnknownHelpTopicHandler]) so the two cannot drift.
//
// In human mode it dumps the parent's help to orient the reader. Under --json
// stdout belongs to the envelope alone, so the dump is suppressed there — a
// page of styled prose where an agent expects one line of JSON is a parse
// error, not a hint.
func unknownSubcommandError(cmd *cli.Command, typed string) error {
	if !IsJSON(cmd) {
		_ = cli.ShowSubcommandHelp(cmd)
	}
	msg := fmt.Sprintf("unknown command %q", typed)
	try := fmt.Sprintf("run '%s --help' to see the available commands", cmd.FullName())

	// No Fix: the corrected name alone is not a resubmittable command line
	// (the reader's arguments and flags aren't reconstructible here), and Fix
	// promises verbatim resubmission.
	if s := suggestSubcommand(cmd, typed); s != "" {
		msg += fmt.Sprintf(". Did you mean %q?", s)
		return Coded(CodeUsage, "%s", msg).WithTry(try)
	}
	// No sibling is close, but the name may be real one level down —
	// `sci convert` when `sci db convert` exists.
	if paths := descendantPaths(cmd, typed); len(paths) > 0 {
		return Coded(CodeUsage, "%s", msg).
			WithTry(fmt.Sprintf("it exists deeper in the tree: %s", strings.Join(paths, ", ")))
	}
	return Coded(CodeUsage, "%s", msg).WithTry(try)
}
