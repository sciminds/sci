package cli

// interrupt.go — cancellation wiring for the docling-running commands
// (content extract, content refresh, extract-lib). Docling runs in its
// own process group (extract.configureProcessGroup) so that canceling
// the batch context kills the whole Python tree — which also means any
// death of sci that BYPASSES context cancellation orphans a GPU-hungry
// docling for nobody. Every way a run can be told to stop therefore maps
// to a ctx cancel:
//
//   - ctrl+c under the progress TUI (raw mode — no SIGINT is generated):
//     uikit.RunWithProgressCtx cancels the work ctx and keeps the display
//     up until the batch winds down.
//   - SIGINT/SIGTERM/SIGHUP outside the TUI (no-TUI extraction, quiet
//     --json runs, a dropped ssh -t session): cmdutil.InterruptContext.
//   - the tty-less remote end of extract.runner=ssh losing its client
//     (no pty → no SIGHUP, only a closed stdin pipe): stdin-EOF watchdog.
//
// Wire extractContext AFTER the confirmation prompt — the watchdog reads
// stdin, and arming it while a prompt might still consume input would
// race the two readers.

import (
	"context"
	"os"
	"time"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"golang.org/x/term"
)

// stdinEOFGrace disarms the watchdog when stdin ends this soon after
// arming: an immediate EOF means the command was started with no stdin
// (</dev/null, agent runners) — not that the ssh client dropped. A real
// drop inside the first two seconds of a minutes-long docling batch is
// not a case worth trading the false positive for.
const stdinEOFGrace = 2 * time.Second

// extractContext derives the context every docling-running command hands
// to extract.Execute / ExecuteBatch: canceled by SIGINT/SIGTERM/SIGHUP,
// and — on the tty-less delegated remote end of runner=ssh — by stdin
// EOF. Defer the returned stop like a context.CancelFunc.
func extractContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := cmdutil.InterruptContext(ctx)
	if watchStdinEOF(os.Getenv(zot.EnvExtractRunner), term.IsTerminal(int(os.Stdin.Fd()))) {
		cmdutil.CancelOnEOF(os.Stdin, stdinEOFGrace, stop)
	}
	return ctx, stop
}

// watchStdinEOF reports whether the stdin-EOF watchdog should arm. Only
// the delegated remote end of runner=ssh qualifies (BuildRemoteArgs sets
// EnvExtractRunner=local on the remote command line), and only without a
// pty — with one (ssh -t), a dropped connection delivers SIGHUP and the
// signal path covers it. Everywhere else stdin belongs to the user.
func watchStdinEOF(runnerEnv string, stdinIsTTY bool) bool {
	return runnerEnv == zot.RunnerLocal && !stdinIsTTY
}

// errExtractInterrupted is what every docling command returns when its
// run was canceled rather than failed: exit 1 with a resume hint instead
// of a wall of per-item "context canceled" noise.
func errExtractInterrupted() error {
	return cmdutil.Coded(cmdutil.CodeRuntime, "interrupted — extraction canceled").
		WithTry("re-run the command to resume; completed extractions are cached and will be skipped")
}
