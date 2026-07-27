package cli

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/urfave/cli/v3"
)

// retiredCommandError reports that a command has moved, carrying a Fix the
// caller can resubmit verbatim.
//
// The extraction verbs moved out of `zot notes` because an extraction is the
// paper's text, not a note: on the live library 4,710 of 4,752 "notes" were
// docling extractions, so `notes list` showed extractions and `notes delete
// --all` trashed them. Re-scoping the noun without moving the verbs would
// have been worse than leaving it alone — `list` would show 42 things while
// `delete --all` still reached 4,710.
//
// extra is appended to the rewritten command (e.g. "--apply", because
// `notes add` posted immediately while `content extract` dry-runs by
// default). Pass nothing when the move is a pure rename.
func retiredCommandError(oldPath, newPath []string, why string, extra ...string) error {
	err := cmdutil.Coded(cmdutil.CodeUsage,
		"`zot %s` has moved to `zot %s`",
		strings.Join(oldPath, " "), strings.Join(newPath, " ")).
		WithTry(why)
	if fix := rewriteCommandFix(os.Args, oldPath, newPath, extra...); fix != "" {
		err = err.WithFix(fix)
	}
	return err
}

// rewriteCommandFix rebuilds the user's command line with one command path
// swapped for another, so a moved verb yields a runnable Fix.
//
// Returns "" when argv is not a recognizable `sci … zot …` invocation or
// doesn't actually contain oldPath — under `go test` os.Args belongs to the
// test binary, and a Fix must be a real command or nothing at all. Same
// discipline as [rewriteFlagFix].
// Any trailing flags are appended at the END rather than spliced in after
// the verb, so the Fix reads the way the docs write it
// (`… content extract KEY --apply`, not `… content extract --apply KEY`).
func rewriteCommandFix(argv []string, oldPath, newPath []string, trailing ...string) string {
	if len(argv) < 2 || !lo.Contains(argv[1:], "zot") || len(oldPath) == 0 {
		return ""
	}
	rest := argv[1:]
	at := indexOfSubslice(rest, oldPath)
	if at < 0 {
		return ""
	}

	quoted := lo.Map(rest, func(arg string, _ int) string { return shellQuote(arg) })
	out := slices.Concat(
		quoted[:at],
		newPath,
		quoted[at+len(oldPath):],
		trailing,
	)
	return "sci " + strings.Join(out, " ")
}

// indexOfSubslice returns the start index of the first occurrence of sub in
// s, or -1. Used to locate a multi-token command path ("notes", "add")
// inside an argv that may carry global flags ahead of it.
func indexOfSubslice(s, sub []string) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// retiredCommand builds a stub command that exists only to explain where its
// verb went. Keeping the name registered is deliberate: urfave would
// otherwise answer `zot notes add` with a bare "command not found", which
// tells the user nothing about where to go next.
func retiredCommand(name, usage string, oldPath, newPath []string, why string, extra ...string) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Description: "Moved to `sci zot " + strings.Join(newPath, " ") + "`.\n\n" +
			why + ".\n\n" +
			"Running this command reports the move and hands back the rewritten\n" +
			"command line, so an agent can resubmit it verbatim.",
		SkipFlagParsing: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			return retiredCommandError(oldPath, newPath, why, extra...)
		},
	}
}
