package main

// The drift gate for the demos `sci help` plays.
//
// A cast is the one piece of documentation that does NOT follow the CLI:
// help text is generated from the command tree, but an asciicast is a
// hand-authored recording of a terminal session, so a moved verb, a
// removed flag or a changed argument order leaves it teaching something
// that now fails. `just doc-coverage` only asks whether a cast EXISTS.
// This asks whether it still parses.
//
// What it cannot check: argument ORDER. `sci db add <csv>... <database>`
// and the reversed form have the same arity, and no gate can read intent
// out of a placeholder name — that one is caught by reading the cast.

import (
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/help"
	"github.com/urfave/cli/v3"
)

// TestCastsInvokeTheBinaryByName pins the prompt. Every cast under
// internal/help/casts demos sci, so every line typed in one starts with
// `sci `. It matters more than house style: `zot` is also the name of a
// different program, so a bare `zot …` prompt reads as naming that one —
// a user copying it either gets "command not found" or silently runs
// something else.
func TestCastsInvokeTheBinaryByName(t *testing.T) {
	cmds, err := help.RecordedCommands()
	if err != nil {
		t.Fatalf("RecordedCommands: %v", err)
	}
	if len(cmds) == 0 {
		t.Fatal("no recorded commands found — the cast parser or the embed is broken")
	}
	for _, rc := range cmds {
		if !strings.HasPrefix(rc.Line, "sci ") {
			t.Errorf("%s: %q does not invoke sci", rc.Cast, rc.Line)
		}
	}
}

// TestCastsResolveAgainstTheCommandTree walks each recorded command down
// the real tree and reports the ways a cast can go stale: a verb that is
// no longer a subcommand, a flag the command no longer declares, and an
// argument count the command would reject.
func TestCastsResolveAgainstTheCommandTree(t *testing.T) {
	cmds, err := help.RecordedCommands()
	if err != nil {
		t.Fatalf("RecordedCommands: %v", err)
	}
	root := buildRoot()

	for _, rc := range cmds {
		tokens := splitCommandLine(rc.Line)
		if len(tokens) == 0 || tokens[0] != "sci" {
			continue // TestCastsInvokeTheBinaryByName owns that failure
		}
		checkRecorded(t, root, rc, tokens[1:])
	}
}

// checkRecorded resolves one recorded command line against cmd's tree.
func checkRecorded(t *testing.T, cmd *cli.Command, rc help.RecordedCommand, tokens []string) {
	t.Helper()

	path := []string{"sci"}
	var positional int

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		if strings.HasPrefix(tok, "-") && tok != "-" {
			name, inline := strings.CutPrefix(tok, "--")
			if !inline {
				name = strings.TrimPrefix(tok, "-")
			}
			name, hasValue := cutFlagValue(name)
			flag := lookupFlag(cmd, name)
			if flag == nil {
				t.Errorf("%s: `%s` passes --%s, which `%s` does not declare",
					rc.Cast, rc.Line, name, strings.Join(path, " "))
				return
			}
			if !hasValue && flagTakesValue(flag) {
				i++ // the next token is this flag's value, not a positional
			}
			continue
		}

		if sub := lookupSub(cmd, tok); sub != nil {
			cmd = sub
			path = append(path, tok)
			positional = 0
			continue
		}

		if len(cmd.Commands) > 0 && cmd.Action == nil {
			t.Errorf("%s: `%s` — `%s` is not a subcommand of `%s`",
				rc.Cast, rc.Line, tok, strings.Join(path, " "))
			return
		}
		positional++
	}

	minArgs, maxArgs := argRange(cmd.ArgsUsage)
	switch {
	case positional < minArgs:
		t.Errorf("%s: `%s` passes %d argument(s); `%s` takes %s",
			rc.Cast, rc.Line, positional, strings.Join(path, " "), cmd.ArgsUsage)
	case maxArgs >= 0 && positional > maxArgs:
		t.Errorf("%s: `%s` passes %d argument(s); `%s` takes %s",
			rc.Cast, rc.Line, positional, strings.Join(path, " "), cmd.ArgsUsage)
	}
}

// lookupSub finds a direct child of cmd by name or alias.
func lookupSub(cmd *cli.Command, name string) *cli.Command {
	for _, sub := range cmd.Commands {
		if sub.Name == name {
			return sub
		}
		for _, a := range sub.Aliases {
			if a == name {
				return sub
			}
		}
	}
	return nil
}

// lookupFlag finds a flag declared on cmd, by name or alias. It does not
// walk the lineage: the test threads the persistent root flags in by
// starting the walk at the root, and urfave resolves the rest the same
// way at run time.
func lookupFlag(cmd *cli.Command, name string) cli.Flag {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return f
			}
		}
	}
	if cmd.Root() != cmd {
		return lookupFlag(cmd.Root(), name)
	}
	return nil
}

// flagTakesValue reports whether the flag consumes the following token.
func flagTakesValue(f cli.Flag) bool {
	_, isBool := f.(*cli.BoolFlag)
	return !isBool
}

// cutFlagValue splits the --flag=value form, reporting whether the value
// rode along in the same token.
func cutFlagValue(name string) (string, bool) {
	if n, _, ok := strings.Cut(name, "="); ok {
		return n, true
	}
	return name, false
}

// argRange reads a command's declared positional arity out of its
// ArgsUsage placeholder string. A trailing "..." means variadic, reported
// as a max of -1.
func argRange(argsUsage string) (minArgs, maxArgs int) {
	fields := strings.Fields(argsUsage)
	if len(fields) == 0 {
		return 0, -1 // undeclared: the command does not promise an arity
	}
	variadic := false
	for _, f := range fields {
		if strings.Contains(f, "...") {
			variadic = true
		}
		if strings.HasPrefix(f, "[") {
			continue // optional
		}
		minArgs++
	}
	if variadic {
		return minArgs, -1
	}
	return minArgs, len(fields)
}

// splitCommandLine tokenizes a recorded line, keeping a double-quoted
// argument as one token so `search "large language models"` counts as one
// positional and not three.
func splitCommandLine(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
			// a quoted empty string is still an argument
			if !inQuote && cur.Len() == 0 {
				out = append(out, "")
			}
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
