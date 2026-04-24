// Package cmdutil provides shared CLI helpers for the sci command tree.
//
// Every sci command returns a [Result] value with both JSON and human-readable
// representations. The [Output] function routes to the correct format based on
// the --json flag.
//
// Confirmation prompts are available in three flavors:
//
//   - [Confirm] prompts with [y/N] (default no)
//   - [ConfirmYes] prompts with [Y/n] (default yes)
//   - [ConfirmOrSkip] wraps Confirm with a skip flag and "cancelled" output,
//     eliminating boilerplate in commands with a --yes flag
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
	return &cli.BoolFlag{Name: "json", Usage: "LLM friendly output", Destination: dst} // lint:no-local — on root command, propagates to all subcommands
}

// IsJSON returns whether --json was set on the command.
func IsJSON(cmd *cli.Command) bool {
	return cmd.Bool("json")
}

// Output writes a Result as JSON or human-readable text depending on the --json flag.
func Output(cmd *cli.Command, r Result) {
	if IsJSON(cmd) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		// CLI output isn't embedded in HTML; keep `<>&` literal so note
		// bodies and abstracts don't turn every tag into `<…` noise.
		enc.SetEscapeHTML(false)
		_ = enc.Encode(r.JSON())
	} else {
		fmt.Print(r.Human())
	}
}

// UsageErrorf returns an error for argument-validation failures.
//
// Behavior depends on whether the user gave us anything to work with:
//
//   - No positional args (bare `sci zot import`) → dump the full help page
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
		return fmt.Errorf("%s", msg)
	}
	usage := cmd.FullName()
	if au := cmd.ArgsUsage; au != "" {
		usage += " " + au
	}
	return fmt.Errorf("%s\n\n  Usage: %s\n  Run '%s --help' for details", msg, usage, cmd.FullName())
}

// ExitCode returns 0 if ok is true, 1 otherwise.
func ExitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
