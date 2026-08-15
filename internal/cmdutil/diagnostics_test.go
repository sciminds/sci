package cmdutil

import (
	"encoding/json"
	"strings"
	"testing"
)

// diagResult is a Result whose Human() body is a document — the shape that
// motivated [Diagnostician] — plus a footer that must not land in it.
type diagResult struct {
	stubResult
	diag string
}

func (d diagResult) Diagnostics() string { return d.diag }

// `sci zot bib paper.qmd --format csl-json > refs.json` writes a document to
// stdout. A stats footer appended to Human() lands inside that document and
// makes it invalid; the footer belongs on stderr, where every other
// diagnostic already goes.
func TestOutput_DiagnosticsGoToStderrNotStdout(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)
	r := diagResult{
		stubResult: stubResult{human: "[{\"id\":\"smith2020\"}]\n"},
		diag:       "12 references, 2 skipped\n",
	}

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { Output(cmd, r) })
	})

	if strings.Contains(stdout, "12 references") {
		t.Errorf("diagnostics leaked into the stdout document:\n%q", stdout)
	}
	if stdout != "[{\"id\":\"smith2020\"}]\n" {
		t.Errorf("stdout should carry the body verbatim, got %q", stdout)
	}
	if !strings.Contains(stderr, "12 references, 2 skipped") {
		t.Errorf("diagnostics missing from stderr, got %q", stderr)
	}
}

// An empty Diagnostics() writes nothing at all — a result can implement the
// interface and stay silent when it has nothing to report.
func TestOutput_EmptyDiagnosticsWriteNothing(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			Output(cmd, diagResult{stubResult: stubResult{human: "body\n"}})
		})
	})

	if stderr != "" {
		t.Errorf("empty diagnostics should write nothing, got %q", stderr)
	}
}

// Under --json the envelope is the whole answer. Diagnostics are prose meant
// for a person watching a pipe; the same counts reach a machine through data.
func TestOutput_DiagnosticsSuppressedUnderJSON(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := diagResult{
		stubResult: stubResult{data: map[string]int{"n": 12}},
		diag:       "12 references, 2 skipped\n",
	}

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { Output(cmd, r) })
	})

	if strings.Contains(stderr, "12 references") {
		t.Errorf("--json should suppress the human diagnostics block, got stderr %q", stderr)
	}
	if strings.Contains(stdout, "12 references") {
		t.Errorf("diagnostics must never enter the envelope, got stdout %q", stdout)
	}
	var env struct {
		OK   bool           `json:"ok"`
		Data map[string]int `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\noutput: %q", err, stdout)
	}
	if !env.OK || env.Data["n"] != 12 {
		t.Errorf("envelope lost its data: %q", stdout)
	}
}

// The capability is opt-in: a plain Result is untouched.
func TestOutput_PlainResultWritesNoDiagnostics(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() { Output(cmd, stubResult{human: "body\n"}) })
	})

	if stderr != "" {
		t.Errorf("a non-Diagnostician result should write nothing to stderr, got %q", stderr)
	}
}
