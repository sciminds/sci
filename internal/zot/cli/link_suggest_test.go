package cli

// Tests for `sci zot link suggest` — the dry-run path end to end, over the
// seedOrientDB fixture where NOTE0001 cites KEY1 (already related) and KEY2
// by DOI plus one zotero:// link that resolves to nothing.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/link"
	"github.com/urfave/cli/v3"
)

func runLinkSuggest(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	t.Cleanup(func() {
		linkSuggestAply = false
		linkSuggestYes = false
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
	runErr := root.Run(context.Background(), slices.Concat([]string{"zot"}, args))

	_ = w.Close()
	return <-done, runErr
}

func suggestResult(t *testing.T, out []byte) link.Result {
	t.Helper()
	var res link.Result
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	return res
}

func TestLinkSuggest_DryRunClassifiesEveryReference(t *testing.T) {
	withOrientConfig(t)

	out, err := runLinkSuggest(t, "--json", "--library", "personal", "link", "suggest", "NOTE0001")
	if err != nil {
		t.Fatalf("link suggest: %v\n%s", err, string(out))
	}
	res := suggestResult(t, out)

	if res.Applied {
		t.Error("Applied = true on a dry run")
	}
	if res.NoteKey != "NOTE0001" {
		t.Errorf("NoteKey = %q, want NOTE0001", res.NoteKey)
	}
	want := link.Totals{Proposed: 1, AlreadyLinked: 1, Unresolved: 1}
	if res.Totals != want {
		t.Errorf("Totals = %+v, want %+v\n%s", res.Totals, want, string(out))
	}

	byKey := map[string]link.Suggestion{}
	for _, s := range res.Suggestions {
		byKey[s.Key] = s
	}
	if got := byKey["KEY2"].Status; got != link.StatusProposed {
		t.Errorf("KEY2 status = %q, want proposed", got)
	}
	if got := byKey["KEY1"].Status; got != link.StatusAlreadyLinked {
		t.Errorf("KEY1 status = %q, want already-linked (the relation is in the fixture)", got)
	}
}

// A dangling zotero:// link surfaces rather than being dropped — the same
// honesty gate `zot bib` applies.
func TestLinkSuggest_DanglingZoteroLinkIsUnresolved(t *testing.T) {
	withOrientConfig(t)

	out, err := runLinkSuggest(t, "--json", "--library", "personal", "link", "suggest", "NOTE0001")
	if err != nil {
		t.Fatalf("link suggest: %v\n%s", err, string(out))
	}
	res := suggestResult(t, out)

	idx := slices.IndexFunc(res.Suggestions, func(s link.Suggestion) bool {
		return s.Status == link.StatusUnresolved
	})
	if idx < 0 {
		t.Fatalf("no unresolved suggestion in %+v", res.Suggestions)
	}
	if !strings.Contains(res.Suggestions[idx].Ref, "ZZZZ9999") {
		t.Errorf("unresolved ref = %q, want the ZZZZ9999 link", res.Suggestions[idx].Ref)
	}
	if res.Suggestions[idx].Reason != "no match" {
		t.Errorf("reason = %q, want %q", res.Suggestions[idx].Reason, "no match")
	}
}

// A docling extraction's references are the PAPER's bibliography, not the
// user's curation — suggesting links off it would be nonsense at scale.
func TestLinkSuggest_RefusesDoclingExtraction(t *testing.T) {
	withOrientConfig(t)

	out, err := runLinkSuggest(t, "--json", "--library", "personal", "link", "suggest", "DOCL0001")
	if err == nil {
		t.Fatalf("link suggest on an extraction succeeded; want a usage error\n%s", string(out))
	}
	coded, ok := err.(*cmdutil.CodedError)
	if !ok {
		t.Fatalf("error = %T (%v), want *cmdutil.CodedError", err, err)
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Try, "content read") {
		t.Errorf("Try = %q, want it to point at `zot content read`", coded.Try)
	}
}

func TestLinkSuggest_HumanRendersEachStatus(t *testing.T) {
	withOrientConfig(t)

	out, err := runLinkSuggest(t, "--library", "personal", "link", "suggest", "NOTE0001")
	if err != nil {
		t.Fatalf("link suggest: %v\n%s", err, string(out))
	}
	for _, want := range []string{
		"NOTE0001", "proposed", "already-linked", "unresolved",
		"KEY2", "Paper Two", "--apply",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, string(out))
		}
	}
}

func TestLinkSuggest_RequiresExactlyOneKey(t *testing.T) {
	withOrientConfig(t)

	if _, err := runLinkSuggest(t, "--library", "personal", "link", "suggest"); err == nil {
		t.Error("link suggest with no key succeeded; want a usage error")
	}
}
