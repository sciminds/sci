package markdb

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// result is implemented by every command's output type.
// JSON() returns structured data for --json mode.
// Human() returns the styled string for terminal display.
type result interface {
	JSON() any
	Human() string
}

// jsonFlag returns a --json BoolFlag wired to the given destination.
func jsonFlag(dst *bool) *cli.BoolFlag {
	return &cli.BoolFlag{Name: "json", Usage: "LLM friendly output", Destination: dst} // lint:no-local — on root command, propagates to all subcommands
}

// isJSON reports whether --json was set on the command.
func isJSON(cmd *cli.Command) bool {
	return cmd.Bool("json")
}

// output writes r as JSON or human-readable text depending on --json.
func output(cmd *cli.Command, r result) {
	if isJSON(cmd) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r.JSON())
	} else {
		fmt.Print(r.Human())
	}
}

// usageErrorf returns an error with the command's usage line and a --help hint.
func usageErrorf(cmd *cli.Command, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	usage := cmd.FullName()
	if au := cmd.ArgsUsage; au != "" {
		usage += " " + au
	}
	return fmt.Errorf("%s\n\n  Usage: %s\n  Run '%s --help' for details", msg, usage, cmd.FullName())
}
