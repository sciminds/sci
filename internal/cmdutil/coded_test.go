package cmdutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr mirrors captureStdout for the human error path.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w

	f()

	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// --- CodedError ---

func TestCodedError_ErrorReturnsMessage(t *testing.T) {
	t.Parallel()
	err := Coded(CodeNotFound, "collection %q not found", "Neuro")
	if err.Error() != `collection "Neuro" not found` {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestCodedError_FluentFixAndTry(t *testing.T) {
	t.Parallel()
	err := Coded(CodeUsage, "missing --library").
		WithFix("sci zot search --library personal foo").
		WithTry("add --library personal or --library shared")
	if err.Fix != "sci zot search --library personal foo" {
		t.Errorf("Fix = %q", err.Fix)
	}
	if err.Try != "add --library personal or --library shared" {
		t.Errorf("Try = %q", err.Try)
	}
}

func TestCodedError_ErrorsAsThroughWrap(t *testing.T) {
	t.Parallel()
	inner := Coded(CodeConflict, "--fulltext and --remote are mutually exclusive")
	wrapped := fmt.Errorf("search: %w", inner)
	coded, ok := errors.AsType[*CodedError](wrapped)
	if !ok {
		t.Fatal("errors.AsType should find the CodedError through %w")
	}
	if coded.Code != CodeConflict {
		t.Errorf("Code = %q, want %q", coded.Code, CodeConflict)
	}
}

// --- HandleError ---

func TestHandleError_JSONEnvelopeOnStdout(t *testing.T) {
	err := Coded(CodeNotFound, "no item with key ABC").
		WithFix("sci zot search --library personal ABC")

	var code int
	out := captureStdout(t, func() {
		code = HandleError(err, true)
	})

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("error envelope should be a single line, got %q", out)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\noutput: %q", err, out)
	}
	if env.OK {
		t.Error("ok should be false")
	}
	if env.Error.Code != "not-found" {
		t.Errorf("code = %q, want not-found", env.Error.Code)
	}
	if env.Error.Message != "no item with key ABC" {
		t.Errorf("message = %q", env.Error.Message)
	}
	if env.Error.Fix != "sci zot search --library personal ABC" {
		t.Errorf("fix = %q", env.Error.Fix)
	}
}

func TestHandleError_JSONOmitsEmptyFixAndTry(t *testing.T) {
	out := captureStdout(t, func() {
		HandleError(Coded(CodeRuntime, "boom"), true)
	})
	if strings.Contains(out, `"fix"`) || strings.Contains(out, `"try"`) {
		t.Errorf("empty fix/try should be omitted, got %q", out)
	}
}

func TestHandleError_UncodedErrorGetsRuntimeCode(t *testing.T) {
	out := captureStdout(t, func() {
		HandleError(errors.New("dial tcp: connection refused"), true)
	})
	if !strings.Contains(out, `"code":"runtime"`) {
		t.Errorf("uncoded error should map to runtime, got %q", out)
	}
}

func TestHandleError_UsageExitsTwo(t *testing.T) {
	for _, code := range []Code{CodeUsage, CodeConflict} {
		got := captureStdout(t, func() {
			if c := HandleError(Coded(code, "bad flags"), true); c != 2 {
				t.Errorf("HandleError(%s) exit = %d, want 2", code, c)
			}
		})
		_ = got
	}
}

func TestHandleError_HumanRendersFixAndTry(t *testing.T) {
	err := Coded(CodeAmbiguous, "3 collections match \"Neu\"").
		WithFix("sci zot collection list --library personal").
		WithTry("use the 8-char collection key instead of the name")

	var code int
	errOut := captureStderr(t, func() {
		code = HandleError(err, false)
	})

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "3 collections match") {
		t.Errorf("missing message: %q", errOut)
	}
	if !strings.Contains(errOut, "sci zot collection list --library personal") {
		t.Errorf("missing fix line: %q", errOut)
	}
	if !strings.Contains(errOut, "use the 8-char collection key") {
		t.Errorf("missing try line: %q", errOut)
	}
}

func TestHandleError_HumanWritesNothingToStdout(t *testing.T) {
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			HandleError(errors.New("plain failure"), false)
		})
	})
	if out != "" {
		t.Errorf("human mode must not write to stdout, got %q", out)
	}
}

// --- Warnings in the Output envelope ---

// warnResult is a Result that also carries warnings.
type warnResult struct {
	stubResult
	warns []Warning
}

func (w warnResult) Warnings() []Warning { return w.warns }

func TestOutput_JSONEnvelopeWrapsData(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := stubResult{data: map[string]string{"key": "value"}, human: "nope"}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	var env struct {
		OK   bool              `json:"ok"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\noutput: %q", err, got)
	}
	if !env.OK {
		t.Error("ok should be true")
	}
	if env.Data["key"] != "value" {
		t.Errorf("data.key = %q, want value", env.Data["key"])
	}
}

func TestOutput_JSONEnvelopeIncludesWarnings(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := warnResult{
		stubResult: stubResult{data: map[string]int{"n": 1}},
		warns: []Warning{{
			Code:    CodeStaleLocal,
			Message: "local DB last synced 107 days ago",
			Fix:     "sci zot search --library personal --remote foo",
		}},
	}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	var env struct {
		Warnings []Warning `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if len(env.Warnings) != 1 {
		t.Fatalf("warnings length = %d, want 1", len(env.Warnings))
	}
	if env.Warnings[0].Code != CodeStaleLocal {
		t.Errorf("warning code = %q", env.Warnings[0].Code)
	}
}

func TestOutput_JSONEnvelopeOmitsEmptyWarnings(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := warnResult{stubResult: stubResult{data: 1}}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if strings.Contains(got, "warnings") {
		t.Errorf("empty warnings should be omitted, got %q", got)
	}
}

func TestOutput_HumanRendersWarnings(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)
	r := warnResult{
		stubResult: stubResult{human: "12 items\n"},
		warns: []Warning{{
			Code:    CodeStaleLocal,
			Message: "local DB last synced 107 days ago",
			Fix:     "sci zot search --remote foo",
		}},
	}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if !strings.Contains(got, "12 items") {
		t.Errorf("missing body: %q", got)
	}
	if !strings.Contains(got, "local DB last synced 107 days ago") {
		t.Errorf("missing warning message: %q", got)
	}
	if !strings.Contains(got, "sci zot search --remote foo") {
		t.Errorf("missing warning fix: %q", got)
	}
}

// --- UsageErrorf returns a coded error ---

func TestUsageErrorf_ReturnsUsageCode(t *testing.T) {
	t.Parallel()
	cmd := newCmd()
	runCmd(t, cmd)
	err := UsageErrorf(cmd, "expected exactly one argument")
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatal("UsageErrorf should return a *CodedError")
	}
	if coded.Code != CodeUsage {
		t.Errorf("Code = %q, want %q", coded.Code, CodeUsage)
	}
}
