package cli

// SSH extraction delegation. When zot.json says extract.runner=ssh, the
// docling-running commands (content extract, content refresh,
// extract-lib) don't run docling here at all — they replay their own
// command line on extract.host via ssh, replacing this process
// (syscall.Exec, the repo's pattern for process-handoff). The remote sci
// resolves its OWN config: the Zotero data_dir and extract dir differ
// per machine even though Zotero desktop sync keeps the libraries
// identical, so nothing path-like from this side may leak across.
// Results flow back through the stores themselves — notes via the
// Zotero Web API, layout dirs via whatever syncs extract_dir.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/lab"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// BuildRemoteArgs constructs the local ssh argv that replays sciArgs
// (os.Args[1:] — everything after the binary name) on host. Pure and
// exported for tests, per the repo's Build*Args convention.
//
// The remote command runs through `exec $SHELL -lc` — a login shell —
// because sshd's non-interactive `$SHELL -c` skips the profile files
// that put sci and docling on PATH. Every argument is quoted with
// [lab.ShellQuote] twice (once per shell that evaluates it), and the
// command is prefixed with SCI_ZOT_RUNNER=local so a remote config
// that itself says runner=ssh can't recurse. interactive adds -t so a
// human sees the remote progress TUI; piped/--json runs stay tty-less
// and take the remote's quiet path with stdout/stderr separated.
func BuildRemoteArgs(host string, sciArgs []string, interactive bool) []string {
	quoted := lo.Map(sciArgs, func(a string, _ int) string { return lab.ShellQuote(a) })
	remoteCmd := zot.EnvExtractRunner + "=" + zot.RunnerLocal + " sci " + strings.Join(quoted, " ")

	args := []string{"ssh"}
	if interactive {
		args = append(args, "-t")
	}
	return append(args, host, "exec $SHELL -lc "+lab.ShellQuote(remoteCmd))
}

// maybeDelegateExtract hands the current invocation to the configured
// ssh host when extract.runner=ssh. Returns handled=false when the
// command should proceed locally (runner local, or no config yet — the
// local path surfaces its own errors). When handled is true the caller
// returns err immediately; on successful delegation syscall.Exec never
// returns, so any return with handled=true is a failure to delegate.
func maybeDelegateExtract(cmd *cli.Command) (handled bool, err error) {
	cfg, err := zot.LoadConfig()
	if err != nil || cfg == nil {
		return false, nil
	}
	runner, host, err := cfg.ExtractRunner()
	if err != nil {
		return true, err
	}
	if runner != zot.RunnerSSH {
		return false, nil
	}

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return true, fmt.Errorf("extract.runner is ssh but no ssh binary on PATH: %w", err)
	}

	// A remote tty is only useful (and only safe — it merges the remote
	// streams into one) when this side is a fully interactive human
	// session: no --json, and stdin/stdout/stderr all terminals.
	interactive := !cmdutil.IsJSON(cmd) &&
		term.IsTerminal(int(os.Stdin.Fd())) &&
		term.IsTerminal(int(os.Stdout.Fd())) &&
		term.IsTerminal(int(os.Stderr.Fd()))

	argv := BuildRemoteArgs(host, os.Args[1:], interactive)
	return true, syscall.Exec(sshPath, argv, os.Environ())
}
