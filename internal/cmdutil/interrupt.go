package cmdutil

// interrupt.go — cooperative cancellation for long-running commands.
// Commands that spawn expensive external work (docling batches) must die
// by context cancellation, never by default signal disposition: the
// subprocess runs in its own process group (see
// internal/zot/extract.configureProcessGroup), so a signal that kills sci
// directly leaves the group running for nobody. These helpers turn every
// "please stop" the OS can deliver into a ctx cancel instead.

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// InterruptContext returns a child of parent that is canceled when the
// process receives SIGINT (Ctrl-C), SIGTERM (kill, shutdown), or SIGHUP
// (terminal closed, ssh -t session dropped). The first signal cancels the
// context and unregisters the handler, so a second Ctrl-C falls through
// to the default disposition and kills the process immediately — the
// escape hatch when cleanup hangs. The returned stop releases the handler
// and cancels the context; defer it like a context.CancelFunc.
func InterruptContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel()
	}
}

// CancelOnEOF starts a watchdog goroutine that calls cancel once r reaches
// EOF (or any read error) — unless the end arrives within grace of the
// call. An immediate EOF means r was empty from the start (a command run
// with </dev/null), not that the peer went away, so the watchdog disarms
// instead of canceling. Bytes read are discarded; only hand it a reader
// nothing else will consume. The goroutine blocks in Read for the life of
// the process when r never ends — callers pass process-lifetime readers
// (stdin), so it never outlives its usefulness.
func CancelOnEOF(r io.Reader, grace time.Duration, cancel func()) {
	go watchEOF(r, grace, cancel)
}

// watchEOF is CancelOnEOF's synchronous body, split out so tests can run
// it without goroutines or sleeps.
func watchEOF(r io.Reader, grace time.Duration, cancel func()) {
	start := time.Now()
	buf := make([]byte, 512)
	for {
		if _, err := r.Read(buf); err != nil {
			if time.Since(start) >= grace {
				cancel()
			}
			return
		}
	}
}
