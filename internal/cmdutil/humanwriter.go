package cmdutil

import (
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
)

// HumanWriter returns the writer every human-mode result should be printed
// to: [os.Stdout] wrapped in a [colorprofile.Writer] that downgrades ANSI to
// whatever the destination can actually display.
//
// Why this exists: human output is a data stream, not just a screen paint.
// `sci zot bib paper.qmd --json | jq` feeds a bibliography to a script, and
// `sci zot search … | rg` is an everyday move. Lip Gloss renders true color
// unconditionally, so without this every piped byte carried escape sequences
// the consumer had to strip. Wrapping the writer fixes it at one choke point
// instead of asking each Result to know whether it's on a terminal.
//
// [colorprofile.NewWriter] queries the writer for TTY support and reads the
// environment, so this also gets us NO_COLOR, CLICOLOR, CLICOLOR_FORCE and
// TERM=dumb for free — all of which sci previously ignored.
//
// It deliberately re-wraps [os.Stdout] on every call rather than caching a
// package-level writer: tests swap os.Stdout for a pipe, and a singleton
// captured at init would keep writing to the real terminal.
func HumanWriter() io.Writer {
	return colorprofile.NewWriter(os.Stdout, os.Environ())
}

// HumanErrWriter is [HumanWriter] for [os.Stderr] — the destination for
// diagnostics (the library banner, update notices, error envelopes) that must
// stay out of the piped stdout stream.
func HumanErrWriter() io.Writer {
	return colorprofile.NewWriter(os.Stderr, os.Environ())
}
