package cli

// Tests for `sci zot item read --doi <doi>` — local DOI-keyed lookup,
// case-insensitive, with friendly errors when the DOI isn't in the
// library. Reuses seedOrientDB / withOrientConfig from info_orient_test.go;
// KEY1 carries DOI 10.1038/nature12373.

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
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// runItemRead is like runOrient/runInfo but tailored for item read so the
// flag-destination state used by readCommand is reset between tests
// (otherwise --doi from one test leaks into the next).
func runItemRead(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	t.Cleanup(func() {
		readDOI = ""
		readRemote = false
	})

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

	var jsonFlag bool
	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(&jsonFlag),
		}, PersistentFlags()...),
		Before:   ValidateLibraryBefore,
		Commands: Commands(),
	}
	full := slices.Concat([]string{"zot"}, args)
	runErr := root.Run(context.Background(), full)

	_ = w.Close()
	stdout := <-done
	return stdout, runErr
}

func TestItemRead_ByDOI_ResolvesToKey(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "--doi", "10.1038/nature12373")
	if err != nil {
		t.Fatalf("item read --doi: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON: %q", string(out))
	}
	var result local.Item
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out[jsonStart:]))
	}
	if result.Key != "KEY1" {
		t.Errorf("Key = %q, want KEY1 (the item carrying DOI 10.1038/nature12373)", result.Key)
	}
}

// TestItemRead_ByDOI_NormalizesPrefixes — agents paste DOIs from browsers,
// PDFs, citation tools. URL forms (https://doi.org/...) and the `doi:`
// scheme prefix all map to the same record.
func TestItemRead_ByDOI_NormalizesPrefixes(t *testing.T) {
	withOrientConfig(t)

	for _, in := range []string{
		"https://doi.org/10.1038/nature12373",
		"http://doi.org/10.1038/nature12373",
		"https://dx.doi.org/10.1038/nature12373",
		"doi:10.1038/nature12373",
		"DOI:10.1038/nature12373",
		"  10.1038/nature12373  ",
	} {
		out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "--doi", in)
		if err != nil {
			t.Fatalf("item read --doi %q: %v\n%s", in, err, string(out))
		}
		jsonStart := bytes.IndexByte(out, '{')
		if jsonStart < 0 {
			t.Fatalf("no JSON for %q: %q", in, string(out))
		}
		var result local.Item
		if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
			t.Fatalf("parse %q: %v", in, err)
		}
		if result.Key != "KEY1" {
			t.Errorf("input %q resolved to %q, want KEY1", in, result.Key)
		}
	}
}

// TestNormalizeDOI is a focused unit test on the prefix-stripping rules
// — quicker feedback than the end-to-end lookup test above.
func TestNormalizeDOI(t *testing.T) {
	cases := map[string]string{
		"10.1/x":                 "10.1/x",
		"https://doi.org/10.1/x": "10.1/x",
		"http://doi.org/10.1/x":  "10.1/x",
		"https://DOI.ORG/10.1/x": "10.1/x", // case-insensitive prefix match
		"doi:10.1/x":             "10.1/x",
		"DOI:10.1/x":             "10.1/x",
		"  10.1/x  ":             "10.1/x",
		"  doi:10.1/x  ":         "10.1/x",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeDOI(in); got != want {
			t.Errorf("normalizeDOI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestItemRead_ByDOI_CaseInsensitive(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "--doi", "10.1038/NATURE12373")
	if err != nil {
		t.Fatalf("item read --doi (uppercase): %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON: %q", string(out))
	}
	var result local.Item
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Key != "KEY1" {
		t.Errorf("Key = %q, want KEY1 (DOI lookup must be case-insensitive)", result.Key)
	}
}

func TestItemRead_ByDOI_MissReturnsHelpfulError(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read", "--doi", "10.0000/does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing DOI")
	}
	msg := err.Error()
	// Error should mention the DOI and point to `find works` for OpenAlex
	// fallback so an agent knows the next step.
	if !strings.Contains(msg, "10.0000/does-not-exist") {
		t.Errorf("err should quote the DOI: %v", err)
	}
	if !strings.Contains(msg, "find works") {
		t.Errorf("err should suggest `find works` as the OpenAlex fallback: %v", err)
	}
}

func TestItemRead_DOIAndKeyConflict_Errors(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read", "KEY1", "--doi", "10.1038/nature12373")
	if err == nil {
		t.Fatal("expected error when both key positional and --doi are supplied")
	}
	if !strings.Contains(err.Error(), "either") {
		t.Errorf("err should explain the mutex: %v", err)
	}
}

func TestItemRead_NoArgsNoDOI_Errors(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read")
	if err == nil {
		t.Fatal("expected error when neither key nor --doi supplied")
	}
	if !strings.Contains(err.Error(), "doi") {
		t.Errorf("err should mention --doi as an option: %v", err)
	}
}

// TestItemChildren_UnknownParentErrors — `item children KEY` previously
// returned "→ KEY has no children" silently when KEY didn't exist,
// matching the empty-children case. Agents then assumed the parent was
// real and leafy. Now it errors with the same not-found message as
// `item read`.
func TestItemChildren_UnknownParentErrors(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "children", "DOESNOTEXIST")
	if err == nil {
		t.Fatal("expected error for nonexistent parent key")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DOESNOTEXIST") || !strings.Contains(msg, "not found") {
		t.Errorf("err should match item-read style: %v", err)
	}
}

// TestItemChildren_ExistingParentNoChildren — leaf items with no
// children still return the empty-children result cleanly (no false
// positive on the parent-existence check).
func TestItemChildren_ExistingParentNoChildren(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "children", "KEY3")
	if err != nil {
		t.Fatalf("item children KEY3: %v\n%s", err, string(out))
	}
	if !bytes.Contains(out, []byte(`"count": 0`)) {
		t.Errorf("expected count:0 for childless real parent, got: %s", out)
	}
}

func TestItemRead_ByPositionalKey_StillWorks(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "KEY3")
	if err != nil {
		t.Fatalf("item read KEY3: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON: %q", string(out))
	}
	var result local.Item
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Key != "KEY3" {
		t.Errorf("Key = %q, want KEY3", result.Key)
	}
}
