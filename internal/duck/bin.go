// Package duck wraps the optional `duckdb` CLI for one-shot data-inspection
// verbs (cols/head/tail/glimpse/shape/summarize/convert/query) over csv,
// tsv, json, jsonl, parquet, xlsx, sqlite, and duckdb files.
//
// We shell out to the binary rather than linking go-duckdb to keep the
// `sci` build CGO-free. Each verb runs duckdb with -json for the structured
// Result payload; human-friendly tables are rendered in-process via
// uikit.RenderTable (see render.go). Snapshot verbs only; no hot loops.
//
// duckdb is an optional dependency declared in
// internal/doctor/BrewfileOptional. Verbs return [ErrNotInstalled] when
// the binary is missing so callers can surface a `sci doctor` hint.
package duck

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// ErrNotInstalled signals that the duckdb CLI was not found on PATH.
// Wrapped errors compare with errors.Is so callers can distinguish this
// from query/syntax failures.
var ErrNotInstalled = errors.New("duckdb not installed — run `sci doctor` to install")

// duckdbBinary is the executable name we look up. Hoisted to a var so
// tests can override if we ever need to.
var duckdbBinary = "duckdb"

// Available reports whether the duckdb CLI can be located on PATH.
func Available() bool {
	_, err := exec.LookPath(duckdbBinary)
	return err == nil
}

// runJSON executes sql via `duckdb -json` against an in-memory database
// and returns stdout. Returns [ErrNotInstalled] (wrapped) if the binary
// is missing.
//
// Read-only safety is enforced in the SQL itself: source files are
// ATTACHed READ_ONLY and verbs only emit SELECT (Convert is the
// documented exception that legitimately writes via COPY ... TO).
func runJSON(sql string) ([]byte, error) {
	if !Available() {
		return nil, ErrNotInstalled
	}
	cmd := exec.Command(duckdbBinary, "-json", "-c", sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrTrim := bytes.TrimSpace(stderr.Bytes())
		if len(stderrTrim) > 0 {
			return nil, fmt.Errorf("duckdb: %s", string(stderrTrim))
		}
		return nil, fmt.Errorf("duckdb: %w", err)
	}
	return stdout.Bytes(), nil
}
