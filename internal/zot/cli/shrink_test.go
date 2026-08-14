package cli

// The fence around the shape of the surface: which verbs answer, which are
// unknown commands, and which flags must fail rather than be ignored.
//
// A verb that is not part of the local read surface is not registered at
// all, so cmdutil.WireNamespaceDefaults rejects it like any other typo. The
// two halves below pin both directions — a read verb must keep its Action,
// and a retired verb must not quietly come back.

import (
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestSurvivingReadVerbsStillAnswer is the half of the shrink that matters
// now: the local read surface is what sci keeps, so every verb below must
// still carry a real Action. A retirement that took a read verb with it
// would otherwise pass unnoticed.
func TestSurvivingReadVerbsStillAnswer(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"search"}, {"browse"}, {"view"}, {"bib"}, {"export"},
		{"info"}, {"setup"}, {"guide"},
		{"item", "read"}, {"item", "list"}, {"item", "children"},
		{"item", "open"}, {"item", "export"},
		{"collection", "list"}, {"collection", "browse"},
		{"tags", "list"}, {"tags", "browse"},
		{"link", "list"},
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

// TestRetiredVerbsAreUnknownCommands fences the inverse of the read surface.
// Every verb below writes a library, extracts paper text, or asks an upstream
// index, so none is registered and the tree must not resolve any of them to
// something that parses. Registering one again by accident (a copy-pasted
// namespace, a revived file) would put a verb back on the public surface
// that this tool has no way to answer.
func TestRetiredVerbsAreUnknownCommands(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"import"},
		{"find"}, {"openalex"}, {"crossref"},
		{"graph"}, {"content"}, {"llm"},
		{"extract"}, {"extract-lib"},
		{"saved-search"},
		{"item", "add"}, {"item", "update"}, {"item", "delete"}, {"item", "attach"},
		{"item", "note"},
		{"collection", "create"}, {"collection", "delete"},
		{"collection", "add"}, {"collection", "remove"},
		{"tags", "add"}, {"tags", "remove"}, {"tags", "delete"},
		{"link", "add"}, {"link", "rm"}, {"link", "suggest"},
		{"notes", "add"}, {"notes", "update"}, {"notes", "delete"},
		{"doctor", "pdfs"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			// Strict resolution, not walkToLeaf: that helper treats an
			// unmatched token as a positional and hands back the parent,
			// which would call `zot item add` "found" because `item` is.
			root := &cli.Command{Name: "zot", Commands: Commands()}
			if commandExists(root, argv) {
				t.Errorf("`zot %s` is registered again — this surface is local and read-only, and that verb is not part of it",
					strings.Join(argv, " "))
			}
		})
	}
}

// TestRetiredFlagsAreGone pins the flag half of the same rule. A removed
// flag has to FAIL rather than be silently ignored, or a script still
// passing it looks like it worked.
//
//   - --content widened a query with a per-library BM25 index this surface
//     does not build.
//   - --remote routed a read to the Zotero Web API, which this surface does
//     not speak.
func TestRetiredFlagsAreGone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path []string
		argv []string
		flag string
	}{
		{path: []string{"search"}, argv: []string{"zot", "search", "--content", "x"}, flag: "content"},
		{path: []string{"search"}, argv: []string{"zot", "search", "--remote", "x"}, flag: "remote"},
		{path: []string{"item", "read"}, argv: []string{"zot", "item", "read", "--remote", "ABC12345"}, flag: "remote"},
		{path: []string{"item", "list"}, argv: []string{"zot", "item", "list", "--remote"}, flag: "remote"},
		{path: []string{"item", "children"}, argv: []string{"zot", "item", "children", "--remote", "ABC12345"}, flag: "remote"},
		{path: []string{"collection", "list"}, argv: []string{"zot", "collection", "list", "--remote"}, flag: "remote"},
		{path: []string{"link", "list"}, argv: []string{"zot", "link", "list", "--remote", "ABC12345"}, flag: "remote"},
	} {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			t.Parallel()

			leaf := walkToLeaf(Commands(), tc.path)
			if leaf == nil {
				t.Fatalf("`zot %s` went missing", strings.Join(tc.path, " "))
			}
			for _, f := range leaf.Flags {
				if slices.Contains(f.Names(), tc.flag) {
					t.Fatalf("--%s is still declared on `zot %s`", tc.flag, strings.Join(tc.path, " "))
				}
			}

			err := runZotTree(t, tc.argv...)
			if err == nil {
				t.Fatalf("`%s` should fail on the removed flag, got nil", strings.Join(tc.argv, " "))
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Errorf("error should name the unknown flag %q, got %q", tc.flag, err)
			}
		})
	}
}
