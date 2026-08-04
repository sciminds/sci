package duck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrNotInstalledMessageMentionsDoctor(t *testing.T) {
	t.Parallel()
	msg := ErrNotInstalled.Error()
	if !strings.Contains(msg, "sci doctor") {
		t.Errorf("ErrNotInstalled.Error() = %q, want it to mention `sci doctor`", msg)
	}
}

func TestRunJSONBasic(t *testing.T) {
	t.Parallel()
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

// TestRunJSONIgnoresUserInitFile pins the -no-init contract: runJSON's
// duckdb subprocess is a protocol channel, and a user's ~/.duckdbrc —
// which may print query results to stdout, switch .mode away from json,
// or LOAD an extension that fails after a duckdb upgrade — must not be
// able to corrupt the payload or fail the call.
func TestRunJSONIgnoresUserInitFile(t *testing.T) {
	requireDuck(t)
	home := t.TempDir()
	rc := "select 'stdout pollution' as noise;\n.mode csv\n"
	if err := os.WriteFile(filepath.Join(home, ".duckdbrc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	out, err := runJSON("SELECT 1 AS x")
	if err != nil {
		t.Fatalf("runJSON with a printing ~/.duckdbrc: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, `"x":1`) {
		t.Errorf("output = %q, want JSON containing `\"x\":1` (mode must survive the rc)", got)
	}
	if strings.Contains(got, "pollution") {
		t.Errorf("output = %q, contains rc stdout pollution — init file was not suppressed", got)
	}
}

func TestRunJSONSyntaxError(t *testing.T) {
	t.Parallel()
	requireDuck(t)
	if _, err := runJSON("THIS IS NOT SQL"); err == nil {
		t.Error("expected error for invalid SQL, got nil")
	}
}

func TestRunJSONNotInstalledReturnsSentinel(t *testing.T) {
	t.Parallel()
	if Available() {
		t.Skip("duckdb is on PATH; cannot test the not-installed path here")
	}
	_, err := runJSON("SELECT 1")
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("got %v, want errors.Is to match ErrNotInstalled", err)
	}
}
