package cli

// Tests for `zot item list` agent-friendliness: the silent-empty hint that
// fires when --collection points at a key the local SQLite doesn't know
// about (typically: collection was created moments ago via the API and
// Zotero desktop hasn't synced yet).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// runZot mirrors runInfo but for any sub-command. Captures stdout so JSON
// can be parsed back. Lives separately because test files in this package
// each maintain their own JSON-flag destination to keep parallel runs
// isolated.
var listJSONOutput bool

func runZot(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	done := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- buf
	}()

	// Minimal command tree — just the leaf we exercise. Pulling in the
	// full Commands() tree would re-bind package-level slice-flag
	// Destinations (--tag, --author, --item, …) that other parallel
	// tests in this package mutate, causing intermittent failures via
	// urfave/cli's SliceBase PreParse re-zeroing.
	itemList := &cli.Command{
		Name: "item",
		Commands: []*cli.Command{
			listCommand(),
		},
	}
	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(&listJSONOutput),
		}, PersistentFlags()...),
		Before:   ValidateLibraryBefore,
		Commands: []*cli.Command{itemList},
	}
	full := slices.Concat([]string{"zot"}, args)
	runErr := root.Run(context.Background(), full)

	_ = w.Close()
	stdout := <-done
	return stdout, runErr
}

func TestItemList_MissingCollection_HintsAboutRemote(t *testing.T) {
	withTestConfig(t, "42", "")

	out, err := runZot(t, "--json", "--library", "personal", "item", "list", "--collection", "NOTREAL1")
	if err != nil {
		t.Fatalf("item list: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	var result zot.ListResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse ListResult: %v\nraw: %s", err, string(out[jsonStart:]))
	}
	if result.Count != 0 {
		t.Fatalf("Count = %d, want 0", result.Count)
	}
	if result.Hint == "" {
		t.Fatal("Hint empty; want a --remote suggestion when collection key is unknown locally")
	}
	if !strings.Contains(result.Hint, "--remote") {
		t.Errorf("Hint = %q, want mention of --remote", result.Hint)
	}
	if !strings.Contains(result.Hint, "NOTREAL1") {
		t.Errorf("Hint = %q, want the missing collection key called out", result.Hint)
	}
}

func TestItemList_NoCollectionFilter_NoHint(t *testing.T) {
	// Bare `item list` against an empty library is legitimately empty —
	// no false positives on the hint.
	withTestConfig(t, "42", "")

	out, err := runZot(t, "--json", "--library", "personal", "item", "list")
	if err != nil {
		t.Fatalf("item list: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	var result zot.ListResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse ListResult: %v", err)
	}
	if result.Hint != "" {
		t.Errorf("Hint = %q, want empty for unfiltered listing", result.Hint)
	}
}
