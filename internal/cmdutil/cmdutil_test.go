package cmdutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// newCmd returns a cli.Command with --json wired up, suitable for testing.
func newCmd() *cli.Command {
	var jsonFlag bool
	cmd := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Destination: &jsonFlag},
		},
	}
	return cmd
}

// runCmd parses flags and runs the command action.
func runCmd(t *testing.T, cmd *cli.Command, args ...string) {
	t.Helper()
	all := append([]string{"test"}, args...)
	if err := cmd.Run(context.Background(), all); err != nil {
		t.Fatal(err)
	}
}

// captureStdout replaces os.Stdout with a pipe, runs f, then restores it and
// returns whatever was written.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// --- IsJSON ---

func TestIsJSON_DefaultFalse(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)
	if IsJSON(cmd) {
		t.Error("IsJSON should return false before the flag is set")
	}
}

func TestIsJSON_TrueWhenSet(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	if !IsJSON(cmd) {
		t.Error("IsJSON should return true after --json is parsed")
	}
}

// --- ExitCode ---

func TestExitCode_TrueReturnsZero(t *testing.T) {
	if got := ExitCode(true); got != 0 {
		t.Errorf("ExitCode(true) = %d, want 0", got)
	}
}

func TestExitCode_FalseReturnsOne(t *testing.T) {
	if got := ExitCode(false); got != 1 {
		t.Errorf("ExitCode(false) = %d, want 1", got)
	}
}

// --- Result interface + Output ---

// stubResult is a minimal Result implementation for testing.
type stubResult struct {
	data  any
	human string
}

func (s stubResult) JSON() any     { return s.data }
func (s stubResult) Human() string { return s.human }

func TestOutput_HumanMode(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd)
	r := stubResult{data: map[string]string{"key": "value"}, human: "hello human"}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if got != "hello human" {
		t.Errorf("human output: got %q, want %q", got, "hello human")
	}
}

func TestOutput_JSONMode(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := stubResult{data: map[string]string{"key": "value"}, human: "should not appear"}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if strings.Contains(got, "should not appear") {
		t.Error("JSON mode should not print the human string")
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("JSON output not valid JSON: %v\noutput: %q", err, got)
	}
	if decoded["key"] != "value" {
		t.Errorf("JSON field 'key': got %q, want %q", decoded["key"], "value")
	}
}

func TestOutput_JSONMode_Indented(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := stubResult{data: map[string]int{"count": 42}, human: ""}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if !strings.Contains(got, "\n") {
		t.Error("JSON output should be indented (contain newlines)")
	}
}

func TestOutput_JSONMode_NilData(t *testing.T) {
	cmd := newCmd()
	runCmd(t, cmd, "--json")
	r := stubResult{data: nil, human: ""}

	got := captureStdout(t, func() {
		Output(cmd, r)
	})

	if strings.TrimSpace(got) != "null" {
		t.Errorf("expected 'null' for nil data, got %q", got)
	}
}

// --- ErrCancelled ---

func TestErrCancelled_IsSentinel(t *testing.T) {
	if ErrCancelled == nil {
		t.Fatal("ErrCancelled should not be nil")
	}
	if ErrCancelled.Error() != "cancelled" {
		t.Errorf("unexpected error message: %q", ErrCancelled.Error())
	}
}

func TestErrCancelled_ErrorsIs(t *testing.T) {
	wrapped := errors.Join(errors.New("outer"), ErrCancelled)
	if !errors.Is(wrapped, ErrCancelled) {
		t.Error("errors.Is should find ErrCancelled inside a joined error")
	}
}

func TestErrCancelled_NotEqualToOtherErrors(t *testing.T) {
	other := errors.New("cancelled") // same text, different identity
	if errors.Is(other, ErrCancelled) {
		t.Error("a different 'cancelled' error should not equal ErrCancelled")
	}
}
