package duck

import (
	"errors"
	"strings"
	"testing"
)

func TestErrNotInstalledMessageMentionsDoctor(t *testing.T) {
	msg := ErrNotInstalled.Error()
	if !strings.Contains(msg, "sci doctor") {
		t.Errorf("ErrNotInstalled.Error() = %q, want it to mention `sci doctor`", msg)
	}
}

func TestRunJSONBasic(t *testing.T) {
	requireDuck(t)
	out, err := runJSON("SELECT 1 AS x")
	if err != nil {
		t.Fatalf("runJSON: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `"x":1`) {
		t.Errorf("runJSON output = %q, want it to contain `\"x\":1`", got)
	}
}

func TestRunBoxBasic(t *testing.T) {
	requireDuck(t)
	out, err := runBox("SELECT 1 AS x")
	if err != nil {
		t.Fatalf("runBox: %v", err)
	}
	if !strings.Contains(out, "x") || !strings.Contains(out, "1") {
		t.Errorf("runBox output = %q, want it to contain x and 1", out)
	}
}

func TestRunJSONSyntaxError(t *testing.T) {
	requireDuck(t)
	if _, err := runJSON("THIS IS NOT SQL"); err == nil {
		t.Error("expected error for invalid SQL, got nil")
	}
}

func TestRunJSONNotInstalledReturnsSentinel(t *testing.T) {
	if Available() {
		t.Skip("duckdb is on PATH; cannot test the not-installed path here")
	}
	_, err := runJSON("SELECT 1")
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("got %v, want errors.Is to match ErrNotInstalled", err)
	}
}
