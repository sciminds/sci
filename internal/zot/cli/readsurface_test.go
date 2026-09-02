package cli

// The fence around the shape of the registered surface, reads and writes:
// every verb sci registers must resolve to something that actually answers.
//
// There is no inverse half. A verb this surface does not offer is not
// registered at all, so cmdutil.WireNamespaceDefaults rejects it exactly like
// a typo — enumerating the verbs that don't exist would assert nothing the
// tree doesn't already enforce for every other string.

import (
	"strings"
	"testing"
)

// TestRegisteredVerbsStillAnswer pins the live surface: every verb below
// must carry a real Action and parse its own flags. A refactor that turned one
// into a stub, or dropped it while rewiring a namespace, would otherwise pass
// unnoticed until someone ran it.
func TestRegisteredVerbsStillAnswer(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"search"}, {"browse"}, {"view"}, {"bib"}, {"export"},
		{"info"}, {"setup"}, {"guide"},
		{"item", "read"}, {"item", "list"}, {"item", "children"},
		{"item", "open"}, {"item", "export"},
		{"item", "add"}, {"item", "update"}, {"item", "delete"}, {"item", "attach"},
		{"collection", "list"}, {"collection", "browse"},
		{"collection", "create"}, {"collection", "update"}, {"collection", "delete"},
		{"collection", "add"}, {"collection", "remove"},
		{"tags", "list"}, {"tags", "browse"}, {"tags", "add"}, {"tags", "remove"},
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
