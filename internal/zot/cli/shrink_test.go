package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/urfave/cli/v3"
)

// zotRoot builds the production tree under a bare `zot` root, the way
// cmd/sci/zot.go mounts it. No Before hook: every assertion below lands
// during flag parsing or inside a stub Action, so no config or database
// is needed.
func zotRoot() *cli.Command {
	return &cli.Command{Name: "zot", Flags: PersistentFlags(), Commands: Commands()}
}

// movedToZot is the table of verbs that left sci for the zot binary when
// sci's Zotero surface shrank to the public local read plane
// (2026-08-12). argv is the command path; zotVerb is the string the
// remedy has to name.
var movedToZot = []struct {
	argv    []string
	zotVerb string
}{
	{argv: []string{"item", "add"}, zotVerb: "zot item add"},
	{argv: []string{"item", "update"}, zotVerb: "zot item update"},
	{argv: []string{"item", "delete"}, zotVerb: "zot item delete"},
	{argv: []string{"item", "attach"}, zotVerb: "zot item attach"},
	{argv: []string{"item", "note", "add"}, zotVerb: "zot item note add"},
	{argv: []string{"item", "note", "update"}, zotVerb: "zot item note update"},
	{argv: []string{"collection", "create"}, zotVerb: "zot collection create"},
	{argv: []string{"collection", "delete"}, zotVerb: "zot collection delete"},
	{argv: []string{"collection", "add"}, zotVerb: "zot collection add"},
	{argv: []string{"collection", "remove"}, zotVerb: "zot collection remove"},
	{argv: []string{"tags", "add"}, zotVerb: "zot tags"},
	{argv: []string{"tags", "remove"}, zotVerb: "zot tags"},
	{argv: []string{"tags", "delete"}, zotVerb: "zot tags"},
	{argv: []string{"find"}, zotVerb: "zot find"},
	{argv: []string{"openalex"}, zotVerb: "zot openalex sync"},
	{argv: []string{"crossref"}, zotVerb: "zot crossref"},
	{argv: []string{"graph"}, zotVerb: "zot cites"},
	{argv: []string{"content"}, zotVerb: "zot read"},
	{argv: []string{"llm"}, zotVerb: "zot read"},
	{argv: []string{"extract"}, zotVerb: "zot extract-lib"},
	{argv: []string{"extract-lib"}, zotVerb: "zot extract-lib"},
	{argv: []string{"doctor", "pdfs"}, zotVerb: "zot doctor pdfs"},
}

// TestMovedVerbsAreRegisteredStubs pins the whole retirement contract at
// once. Each verb stays REGISTERED — urfave would otherwise answer with a
// bare "command not found", which teaches nothing about where the verb
// went — and each refuses with CodeUsage naming its replacement in Try.
//
// Try, never Fix: the zot binary is a different program and is absent
// from lab machines, so a rewritten command line would hand back
// something that cannot run. Error.Fix is verbatim-runnable or absent.
func TestMovedVerbsAreRegisteredStubs(t *testing.T) {
	t.Parallel()
	for _, tc := range movedToZot {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			t.Parallel()

			leaf := walkToLeaf(Commands(), tc.argv)
			if leaf == nil {
				t.Fatalf("`zot %s` must stay registered so the move is discoverable",
					strings.Join(tc.argv, " "))
			}
			if !leaf.SkipFlagParsing {
				t.Errorf("`zot %s` must SkipFlagParsing so old flags reach the explanation",
					strings.Join(tc.argv, " "))
			}

			argv := slices.Concat([]string{"zot"}, tc.argv)
			err := zotRoot().Run(context.Background(), argv)
			if err == nil {
				t.Fatalf("`%s` should refuse, got nil", strings.Join(argv, " "))
			}
			coded, ok := errors.AsType[*cmdutil.CodedError](err)
			if !ok {
				t.Fatalf("want CodedError, got %T: %v", err, err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want %q", coded.Code, cmdutil.CodeUsage)
			}
			if coded.Fix != "" {
				t.Errorf("Fix must stay empty — zot is not installed here; got %q", coded.Fix)
			}
			if !strings.Contains(coded.Try, tc.zotVerb) {
				t.Errorf("Try must name %q, got %q", tc.zotVerb, coded.Try)
			}
		})
	}
}

// TestMovedVerbsSwallowTheirOldFlags is the flag-parsing half: a script
// still passing the write flags must reach the explanation rather than a
// bare "flag provided but not defined", which reads like a typo instead
// of a move.
func TestMovedVerbsSwallowTheirOldFlags(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"item", "add", "--type", "journalArticle", "--title", "X", "--openalex", "10.1/2"},
		{"item", "update", "ABC12345", "--from-json", "plan.ndjson"},
		{"item", "attach", "ABC12345", "/tmp/x.pdf", "--skip-existing"},
		{"collection", "add", "--from-file", "keys.txt", "COLLXXX1"},
		{"tags", "add", "ABC12345", "neuroimaging"},
		{"tags", "delete", "deprecated", "--yes"},
		{"saved-search", "create", "Recent ML", "--condition", "tag:is:ml", "--any"},
		{"saved-search", "delete", "ABCD1234", "--yes"},
		{"find", "works", "--filter", "type=article", "llm"},
		{"openalex", "sync", "--apply", "--limit", "10"},
		{"crossref", "works", "--apply"},
		{"graph", "cites", "ABC12345", "--limit", "50"},
		{"content", "build", "--rebuild"},
		{"llm", "query", "-s", "transformers", "--", ".h2"},
		{"extract-lib", "--apply", "--jobs", "2"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			err := zotRoot().Run(context.Background(), slices.Concat([]string{"zot"}, argv))
			coded, ok := errors.AsType[*cmdutil.CodedError](err)
			if !ok {
				t.Fatalf("want CodedError, got %T: %v", err, err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want %q", coded.Code, cmdutil.CodeUsage)
			}
		})
	}
}

// TestSurvivingReadVerbsStillAnswer is the other half of the shrink: the
// local read surface is what sci keeps, so every verb below must still
// carry a real Action. A retirement that took a read verb with it would
// pass every test above and still be wrong.
func TestSurvivingReadVerbsStillAnswer(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"search"}, {"browse"}, {"view"}, {"bib"}, {"export"}, {"import"},
		{"info"}, {"setup"}, {"guide"},
		{"item", "read"}, {"item", "list"}, {"item", "children"},
		{"item", "open"}, {"item", "export"},
		{"item", "note", "read"}, {"item", "note", "list"},
		{"collection", "list"}, {"collection", "browse"},
		{"saved-search", "list"}, {"saved-search", "show"},
		{"tags", "list"}, {"tags", "browse"},
		{"notes", "list"}, {"notes", "read"},
		{"doctor", "invalid"}, {"doctor", "missing"}, {"doctor", "orphans"},
		{"doctor", "duplicates"}, {"doctor", "citekeys"}, {"doctor", "dois"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			leaf := walkToLeaf(Commands(), argv)
			if leaf == nil {
				t.Fatalf("`zot %s` went missing", strings.Join(argv, " "))
			}
			if leaf.Action == nil {
				t.Fatalf("`zot %s` lost its Action", strings.Join(argv, " "))
			}
			if leaf.SkipFlagParsing {
				t.Errorf("`zot %s` is a live verb, not a stub", strings.Join(argv, " "))
			}
		})
	}
}

// TestRetiredContentFlagsAreGone pins the search half of the content
// retirement. --content widened a query with the per-library BM25 index
// that retired with `zot content`; a removed flag has to FAIL rather than
// be silently ignored, or a script still passing it looks like it worked.
func TestRetiredContentFlagsAreGone(t *testing.T) {
	t.Parallel()
	leaf := walkToLeaf(Commands(), []string{"search"})
	if leaf == nil {
		t.Fatal("`zot search` went missing")
	}
	for _, f := range leaf.Flags {
		if slices.Contains(f.Names(), "content") {
			t.Fatal("--content is still declared on `zot search`")
		}
	}
	err := zotRoot().Run(context.Background(), []string{"zot", "search", "--content", "x"})
	if err == nil {
		t.Fatal("`search --content` should fail on the removed flag, got nil")
	}
	if !strings.Contains(err.Error(), "content") {
		t.Errorf("error should name the unknown flag, got %q", err)
	}
}

// retiredOutright is the table of verbs that left sci with no home in any
// binary. They are a different kind of retirement from [movedToZot] and
// need their own assertion: the remedy is prose, so a Try that names a
// `zot` command would send the caller somewhere that has no such verb.
var retiredOutright = []struct {
	argv   []string
	remedy string
}{
	{argv: []string{"saved-search", "create"}, remedy: "Zotero desktop"},
	{argv: []string{"saved-search", "update"}, remedy: "Zotero desktop"},
	{argv: []string{"saved-search", "delete"}, remedy: "Zotero desktop"},
}

// TestRetiredOutrightVerbsPointAtTheDesktop pins the second retirement
// shape. A saved search is a stored QUERY, and the Zotero Web API can hold
// its definition but cannot evaluate it — only the desktop client runs one.
// So a write verb for a thing only the desktop can use belongs to the
// desktop's own UI, and the stub has to say that rather than hand the
// caller a `zot` command that does not exist.
func TestRetiredOutrightVerbsPointAtTheDesktop(t *testing.T) {
	t.Parallel()
	for _, tc := range retiredOutright {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			t.Parallel()

			leaf := walkToLeaf(Commands(), tc.argv)
			if leaf == nil {
				t.Fatalf("`zot %s` must stay registered so the retirement is discoverable",
					strings.Join(tc.argv, " "))
			}
			if !leaf.SkipFlagParsing {
				t.Errorf("`zot %s` must SkipFlagParsing so old flags reach the explanation",
					strings.Join(tc.argv, " "))
			}

			err := zotRoot().Run(context.Background(), slices.Concat([]string{"zot"}, tc.argv))
			if err == nil {
				t.Fatalf("`zot %s` should refuse, got nil", strings.Join(tc.argv, " "))
			}
			coded, ok := errors.AsType[*cmdutil.CodedError](err)
			if !ok {
				t.Fatalf("want CodedError, got %T: %v", err, err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want %q", coded.Code, cmdutil.CodeUsage)
			}
			if coded.Fix != "" {
				t.Errorf("Fix must stay empty — the remedy is an app, not a command; got %q", coded.Fix)
			}
			if !strings.Contains(coded.Try, tc.remedy) {
				t.Errorf("Try must name %q, got %q", tc.remedy, coded.Try)
			}
			// The moved shape ends "on a machine that has zot installed,
			// run `zot …`". Borrowing that sentence here would send the
			// caller to a second binary that has no such verb either.
			if strings.Contains(coded.Try, "zot installed") {
				t.Errorf("Try routes to the zot binary, which has no home for this verb: %q", coded.Try)
			}
		})
	}
}
