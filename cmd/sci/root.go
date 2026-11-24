// Package main is the entry point for the sci CLI, a toolkit for managing
// Python-based scientific computing projects (environments, notebooks,
// databases, sharing, and more).
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/selfupdate"
	dbtui "github.com/sciminds/sci/internal/tui/dbtui/app"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/version"
	"github.com/urfave/cli/v3"
)

var jsonOutput bool

// versionResult is what both `sci --version` and `sci version` return.
//
// The version has to be reachable at all: sci installs by curl and updates
// itself, so "which build am I on?" is the first question of every bug
// report, and until this existed there was no way to ask it — version.String()
// was live code with no caller.
type versionResult struct{}

func (versionResult) JSON() any {
	return map[string]any{
		"version": cmp.Or(version.Version, "dev"),
		"commit":  version.Commit,
		"dev":     version.IsDev(),
	}
}

func (versionResult) Human() string { return "sci " + version.String() + "\n" }

// versionCommand is the `sci version` spelling of the --version flag.
//
// It exists because `sci version` is what people (and agents) type first, and
// answering "unknown command" to it is a dead end. Visible, so it is listed
// wherever the other Maintenance commands are — which is why it also carries
// an overview in internal/help/descs.go, the one thing `sci help` cannot
// derive from this tree.
func versionCommand() *cli.Command {
	return &cli.Command{
		Name:        "version",
		Usage:       "Print the sci version",
		Description: "$ sci version\n$ sci version --json",
		Category:    "Maintenance",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cmdutil.Output(cmd, versionResult{})
			return nil
		},
	}
}

// humanHelpWriter is the root command's [cli.Command.Writer], and so the
// destination of every `--help` page urfave/cli prints (its help printer
// always writes to `cmd.Root().Writer`).
//
// A help page is human output — `sci zot --help | rg search` is an everyday
// move — so it belongs on stdout, through [cmdutil.HumanWriter], like every
// other human-mode stream. Until it was routed here it went to urfave's raw
// writer, which meant `NO_COLOR=1 sci zot --help | …` handed the reader 27
// escape sequences it never asked for.
//
// It re-resolves the writer on every Write instead of holding one, for the
// reason [cmdutil.HumanWriter] gives: the profile is detected from the live
// os.Stdout, and a writer captured at buildRoot() would keep painting the real
// terminal after a test swapped it for a pipe. Each Write carries whole
// rendered lines, so nothing is split across the profile downgrade.
type humanHelpWriter struct{}

func (humanHelpWriter) Write(p []byte) (int, error) {
	return cmdutil.HumanWriter().Write(p)
}

// versionPrinterOnce guards the one-time cli.VersionPrinter swap: it is a
// package global in urfave/cli, and buildRoot runs once per test.
var versionPrinterOnce sync.Once

// useVersionResult routes urfave's --version handling through
// [cmdutil.Output], so `sci --version --json` answers in the same envelope as
// every other command instead of printing a bare line.
func useVersionResult() {
	versionPrinterOnce.Do(func() {
		cli.VersionPrinter = func(cmd *cli.Command) { cmdutil.Output(cmd, versionResult{}) }
	})
}

func buildRoot() *cli.Command {
	// Update notice: rendered in Before (not After) so it surfaces even for
	// subcommands that end in syscall.Exec (REPL, marimo, quarto, lab
	// get/put/connect, py, proj/exec) or sit inside an alt-screen TUI —
	// urfave/cli's After hook is unreachable from any of those paths.
	// Trade-off: the notice prints at the top of output instead of the
	// bottom. For exec/TUI flows the line stays on the main-screen
	// scrollback and is restored when the user exits.
	root := &cli.Command{
		Name:    "sci",
		Usage:   "Your scientific computing toolkit",
		Version: version.String(),
		Description: "# agents: every command accepts --json ({ok,data,warnings} / {ok:false,error:{code,message,fix,try}});\n" +
			"# resubmit error.fix verbatim when present. For Zotero work run `sci zot guide --json` once first.",
		Flags: []cli.Flag{
			// lint:destination-ok — the sole flag destination left in this
			// package: main() reads jsonOutput at the cmdutil.HandleError
			// call below, after root.Run() has returned and no *cli.Command
			// is in scope. Every other flag is read via cmd.<Type>("name").
			cmdutil.JSONFlag(&jsonOutput),
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			selfupdate.SpawnDetachedRefresh()
			// Suppress for --json (machine output) and for `sci update`
			// itself (the user is already running the updater). At root's
			// Before, cmd is the root command — peek at the first positional
			// arg to identify the resolved subcommand.
			if !cmdutil.IsJSON(cmd) && cmd.Args().First() != "update" {
				if notice := selfupdate.ReadCachedNotice(); notice != "" {
					// HumanErrWriter, not os.Stderr: the arrow glyph is
					// styled, and a notice piped into a log should arrive as
					// plain text like every other diagnostic.
					_, _ = fmt.Fprintf(cmdutil.HumanErrWriter(), "\n  %s %s\n", uikit.SymArrow, notice)
					selfupdate.MarkNoticeShown()
				}
			}
			return ctx, nil
		},
		// No Action: cmdutil.WireNamespaceDefaults installs the shared
		// namespace one, which shows this help page for a human and refuses
		// with a usage envelope under --json.
		Suggest:         true,
		HideHelpCommand: true,
		// lo.Compact filters out any nil entries — used on Linux where
		// platform-specific commands (e.g. `sci tools`) return nil from
		// their stubbed constructor.
		Commands: lo.Compact([]*cli.Command{
			// What Can I Do?
			helpCommand(),
			// Getting Started
			learnCommand(),
			setupCommand(),
			// Commands
			cloudCommand(),
			dbCommand(),
			labCommand(),
			projCommand(),
			pyCommand(),
			vidCommand(),
			viewCommand(),
			zotCommand(),
			// Maintenance
			toolsCommand(),
			doctorCommand(),
			updateCommand(),
			versionCommand(),
		}),
	}
	// Every `--help` page urfave prints goes to the root's Writer; see
	// humanHelpWriter for why it is not os.Stdout.
	root.Writer = humanHelpWriter{}
	cmdutil.SetupHelp(root)
	cmdutil.WireNamespaceDefaults(root)
	useVersionResult()
	return root
}

func main() {
	// Detached refresh trampoline: when [selfupdate.SpawnDetachedRefresh]
	// re-execs this binary with the sentinel env var set, do the (slow)
	// update check and exit. This bypasses every urfave/cli code path so
	// the child cannot accidentally execute a user-facing command.
	if os.Getenv(selfupdate.InternalRefreshEnv) == "1" {
		selfupdate.RefreshCache()
		return
	}

	root := buildRoot()
	if err := root.Run(context.Background(), os.Args); err != nil {
		if errors.Is(err, dbtui.ErrInterrupted) {
			os.Exit(130)
		}
		// jsonOutput is only assigned once flag parsing succeeds. When it is
		// the parse that failed — `sci zot info --nosuchflag --json` — the
		// destination is still false even though the caller asked for machine
		// output, so fall back to reading the intent straight off argv.
		// Without this the one failure an agent most needs an envelope for is
		// the one it never gets.
		os.Exit(cmdutil.HandleError(err, jsonOutput || cmdutil.JSONRequested(os.Args)))
	}
}
