// Package cmdutil provides shared CLI helpers for the sci command tree.
//
// Every sci command returns a [Result] value with both JSON and human-readable
// representations. The [Output] function routes to the correct format based on
// the --json flag.
//
// # The --json contract (agent-facing)
//
// Under --json, sci speaks one stream (stdout) and one shape:
//
//	{"ok": true,  "data": {...}, "warnings": [{"code","message","fix"}]}  // success
//	{"ok": false, "error": {"code","message","fix","try"}}                // failure
//
// Every key shown is always present on its shape: warnings is an empty array
// when a command has nothing to caveat, so `.warnings` is safe to read
// unconditionally.
//
// Failures carry a [Code] from a closed vocabulary plus, where the correction
// is unambiguous, a Fix command to resubmit verbatim ([CodedError]). Exit
// codes partition by remediation: 2 = rewrite the command line and retry
// (usage/conflict), 1 = everything else. Successful results may volunteer
// [Warning] values (data-quality and freshness caveats) via [Warner] —
// agents should act on warnings before trusting the data.
//
// The contract holds for the command line itself, not just for commands that
// ran: a mistyped flag, a namespace invoked with no subcommand and an unknown
// help topic all produce the same failure envelope and exit 2. See usage.go
// for how urfave/cli's own refusals are translated.
//
// # Streams
//
// stdout carries the answer, stderr carries diagnostics. Human results go
// through [HumanWriter] and error envelopes through [HumanErrWriter], both of
// which strip ANSI when the destination isn't a terminal and honor
// NO_COLOR / CLICOLOR_FORCE. A Result whose human form is a *document* can
// implement [Diagnostician] to print a footer to stderr while stdout stays
// pipeable.
//
// Confirmation prompts are available in four flavors:
//
//   - [Confirm] prompts with [y/N] (default no)
//   - [ConfirmYes] prompts with [Y/n] (default yes)
//   - [ConfirmRequired] prompts default-yes with an emphasized
//     "Yes (required)" label for prerequisites the user almost certainly wants
//   - [ConfirmOrSkip] wraps Confirm with a skip flag and "cancelled" output,
//     eliminating boilerplate in commands with a --yes flag
//
// SCI_ASSUME=yes|no answers all confirmations non-interactively.
//
// Usage:
//
//	func runMyCmd(ctx context.Context, cmd *cli.Command) error {
//	    result, err := doWork()
//	    if err != nil {
//	        return err
//	    }
//	    cmdutil.Output(cmd, result)
//	    return nil
//	}
package cmdutil

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

// Result is the interface every command's output must implement.
// JSON() returns the structured data for --json mode.
// Human() returns the styled string for terminal display.
type Result interface {
	JSON() any
	Human() string
}

// JSONFlag returns a --json BoolFlag that can be included in a command's Flags slice.
func JSONFlag(dst *bool) *cli.BoolFlag {
	return &cli.BoolFlag{Name: "json", Usage: "LLM friendly output", Destination: dst} // lint:no-local — on root command, propagates to all subcommands; lint:destination-ok — main() reads the destination after root.Run() returns, where no *cli.Command is in scope
}

// IsJSON returns whether --json was set on the command.
func IsJSON(cmd *cli.Command) bool {
	return cmd.Bool("json")
}

// Diagnostician is implemented by Results whose Human() output is a
// *document* — something a caller redirects into a file or pipes to another
// program — and which also have something to say about the run itself.
//
// [Output] renders Diagnostics() to [HumanErrWriter] (stderr) so the footer
// never lands inside the body. Without it, `sci zot bib paper.qmd --format
// csl-json > refs.json` produces a file whose last lines are a human stats
// block, which is to say invalid JSON.
//
// Opt-in: implement it only on the results that need it; every other Result
// is unaffected. Return "" to say nothing on a given run.
//
// Under --json the diagnostics are suppressed entirely rather than folded
// into warnings. A [Warning] is a typed, machine-branchable caveat with a
// [Code] from a closed vocabulary; a diagnostics block is styled prose for a
// person watching a pipe. Promoting one to the other would put untyped text
// in a field agents branch on, and the same counts already reach a machine
// through the envelope's data.
type Diagnostician interface {
	Diagnostics() string
}

// Output writes a Result as JSON or human-readable text depending on the
// --json flag.
//
// JSON mode wraps every result in the success envelope
//
//	{"ok": true, "data": <r.JSON()>, "warnings": [...]}
//
// so agents always parse one shape; failures emit the matching
// {"ok":false,"error":{...}} envelope via [HandleError]. All three keys are
// always present — warnings is an empty array when there is nothing to say,
// never absent and never null, so a consumer can read `.warnings`
// unconditionally.
//
// Results that implement [Warner] get their warnings merged into the envelope
// (JSON) or appended as ⚠ lines (human) — warnings never change the exit
// code. Results that implement [Diagnostician] additionally emit a stderr
// block in human mode, keeping stdout a clean document.
func Output(cmd *cli.Command, r Result) {
	// Non-nil so the envelope's warnings key marshals as [] rather than null.
	warnings := []Warning{}
	if w, ok := r.(Warner); ok {
		warnings = append(warnings, w.Warnings()...)
	}
	if IsJSON(cmd) {
		env := struct {
			OK       bool      `json:"ok"`
			Data     any       `json:"data"`
			Warnings []Warning `json:"warnings"`
		}{OK: true, Data: r.JSON(), Warnings: warnings}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		// CLI output isn't embedded in HTML; keep `<>&` literal so note
		// bodies and abstracts don't turn every tag into `<…` noise.
		enc.SetEscapeHTML(false)
		_ = enc.Encode(env)
		return
	}

	out := HumanWriter()
	_, _ = fmt.Fprint(out, r.Human())
	for _, w := range warnings {
		_, _ = fmt.Fprintf(out, "\n  %s %s\n", uikit.SymWarn, w.Message)
		if w.Fix != "" {
			_, _ = fmt.Fprintf(out, "    %s fix: %s\n", uikit.SymArrow, w.Fix)
		}
	}
	if d, ok := r.(Diagnostician); ok {
		if block := d.Diagnostics(); block != "" {
			_, _ = fmt.Fprint(HumanErrWriter(), block)
		}
	}
}

// UsageErrorf returns an error for argument-validation failures.
//
// Behavior depends on whether the user gave us anything to work with:
//
//   - No positional args (bare `sci zot search`) → dump the full help page
//     first, then return a short error. The user had nothing to go on; show
//     them what the command expects the same way `--help` would.
//   - Args present (flag conflict, wrong count) → keep the terse "Usage: …"
//     tail. A wall of help would bury the real error.
//   - --json mode → never dump styled help (the consumer is a machine);
//     always use the terse form.
//
// Parent namespaces (`sci zot`, `sci zot item`, …) don't hit this path —
// urfave's auto-help fires for them because they have no Action.
func UsageErrorf(cmd *cli.Command, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if cmd.Args().Len() == 0 && !IsJSON(cmd) {
		_ = cli.ShowSubcommandHelp(cmd)
		return Coded(CodeUsage, "%s", msg)
	}
	usage := cmd.FullName()
	if au := cmd.ArgsUsage; au != "" {
		usage += " " + au
	}
	return Coded(CodeUsage, "%s", msg).
		WithTry(fmt.Sprintf("usage: %s — run '%s --help' for details", usage, cmd.FullName()))
}

// ExitCode returns 0 if ok is true, 1 otherwise.
func ExitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
