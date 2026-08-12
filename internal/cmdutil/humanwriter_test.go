package cmdutil

import (
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/uikit"
)

// A styled Result piped into a non-terminal (the pipe captureStdout installs)
// must come out as plain text. Human output is routinely piped — `sci zot
// content read KEY | llm` hands a paper to a model — and escape sequences in
// that stream are noise the consumer has to strip.
func TestOutput_HumanMode_StripsANSIWhenNotATerminal(t *testing.T) {
	styled := uikit.TUI.TextBlue().Render("hyperscanning")
	if !strings.Contains(styled, "\x1b[") {
		t.Fatal("precondition: uikit style produced no escape sequence; test proves nothing")
	}

	// Parse outside the capture: this command has no Action, so cmd.Run
	// dumps urfave's styled help page and that ANSI is not what we're testing.
	cmd := newCmd()
	runCmd(t, cmd)

	out := captureStdout(t, func() {
		Output(cmd, stubResult{human: styled + "\n"})
	})

	if strings.Contains(out, "\x1b[") {
		t.Errorf("human output kept ANSI escapes on a non-terminal:\n%q", out)
	}
	if !strings.Contains(out, "hyperscanning") {
		t.Errorf("stripping ANSI also ate the text:\n%q", out)
	}
}

// The warning lines Output appends go through the same writer — a warning is
// as much a part of the piped stream as the body.
func TestOutput_Warnings_StripANSIWhenNotATerminal(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)

	out := captureStdout(t, func() {
		Output(cmd, warnResult{
			stubResult: stubResult{human: "body\n"},
			warns:      []Warning{{Message: "index is stale", Fix: "sci zot content build"}},
		})
	})

	if strings.Contains(out, "\x1b[") {
		t.Errorf("warning line kept ANSI escapes on a non-terminal:\n%q", out)
	}
	if !strings.Contains(out, "index is stale") {
		t.Errorf("warning message missing:\n%q", out)
	}
}
