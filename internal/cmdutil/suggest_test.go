package cmdutil

import (
	"io"
	"testing"

	"github.com/urfave/cli/v3"
)

// --- similarity scoring ---

// The scorer must treat a swapped pair of letters as a near-miss. Jaro-Winkler
// (what urfave/cli ships) scores "zto" closer to "tools" than to "zot", which
// is how `sci zto` came to advise the wrong command.
func TestSimilarity_TranspositionIsANearMiss(t *testing.T) {
	t.Parallel()
	swapped := similarity("zto", "zot")
	unrelated := similarity("zto", "tools")
	if swapped <= unrelated {
		t.Errorf("similarity(zto,zot)=%.3f should beat similarity(zto,tools)=%.3f", swapped, unrelated)
	}
	if swapped < minSuggestionSimilarity {
		t.Errorf("similarity(zto,zot)=%.3f fell below the suggestion threshold %.2f", swapped, minSuggestionSimilarity)
	}
}

func TestSimilarity_IdenticalIsOne(t *testing.T) {
	t.Parallel()
	if got := similarity("zot", "zot"); got != 1 {
		t.Errorf("similarity of identical strings = %.3f, want 1", got)
	}
}

// --- bestMatch ---

func TestBestMatch_PicksTheRealTypo(t *testing.T) {
	t.Parallel()
	names := []string{"cloud", "db", "doctor", "learn", "search", "tools", "view", "zot"}
	cases := map[string]string{
		"zto":    "zot",
		"serach": "search",
		"viwe":   "view",
		"doctr":  "doctor",
	}
	for typed, want := range cases {
		if got := bestMatch(names, typed); got != want {
			t.Errorf("bestMatch(%q) = %q, want %q", typed, got, want)
		}
	}
}

// A suggestion nobody typed a typo of is worse than no suggestion at all:
// "Did you mean h?" sends the reader chasing an alias that isn't a command.
func TestBestMatch_SuppressesDistantMatches(t *testing.T) {
	t.Parallel()
	names := []string{"cloud", "db", "doctor", "help", "h", "learn", "view", "zot"}
	for _, typed := range []string{"nosuchcommand", "wxyz", "hyperscanning"} {
		if got := bestMatch(names, typed); got != "" {
			t.Errorf("bestMatch(%q) = %q, want no suggestion", typed, got)
		}
	}
}

func TestBestMatch_IgnoresSingleLetterCandidates(t *testing.T) {
	t.Parallel()
	// "h" is one edit from "a", which would score 0.5 — still below the bar —
	// but a one-letter alias is never a useful thing to suggest, so it must
	// not even be a candidate.
	if got := bestMatch([]string{"h", "v"}, "hh"); got != "" {
		t.Errorf("bestMatch = %q, want no suggestion for a single-letter alias", got)
	}
}

// --- suggestSubcommand ---

func TestSuggestSubcommand_UsesVisibleChildren(t *testing.T) {
	t.Parallel()
	cmd := &cli.Command{
		Name: "sci",
		Commands: []*cli.Command{
			{Name: "zot"},
			{Name: "tools"},
			{Name: "secret", Hidden: true},
		},
	}
	if got := suggestSubcommand(cmd, "zto"); got != "zot" {
		t.Errorf("suggestSubcommand(zto) = %q, want zot", got)
	}
	if got := suggestSubcommand(cmd, "secrt"); got != "" {
		t.Errorf("hidden commands must not be suggested, got %q", got)
	}
}

// --- descendant lookup ---

// `sci convert` is plausible input because `sci db convert`, `sci py convert`
// and `sci vid convert` all exist. Nothing at the top level is spelled
// anything like it, so the only useful answer is the deeper path(s).
func TestDescendantPaths_FindsNestedExactMatches(t *testing.T) {
	t.Parallel()
	root := &cli.Command{
		Name:   "sci",
		Writer: io.Discard,
		Commands: []*cli.Command{
			{Name: "db", Commands: []*cli.Command{{Name: "convert"}, {Name: "query"}}},
			{Name: "py", Commands: []*cli.Command{{Name: "convert"}}},
			{Name: "cloud", Commands: []*cli.Command{{Name: "ls"}}},
		},
	}
	// Parent links are wired by urfave at Run time; do it by hand here.
	if err := root.Run(t.Context(), []string{"sci"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := descendantPaths(root, "convert")
	want := []string{"sci db convert", "sci py convert"}
	if len(got) != len(want) {
		t.Fatalf("descendantPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("descendantPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// `sci serach` used to suggest `learn`. The command the reader wants is
// `sci zot search`, one level down, so no top-level suggestion can be right.
func TestDescendantPaths_FindsNestedNearMisses(t *testing.T) {
	t.Parallel()
	root := &cli.Command{
		Name:   "sci",
		Writer: io.Discard,
		Commands: []*cli.Command{
			{Name: "zot", Commands: []*cli.Command{{Name: "search"}, {Name: "bib"}}},
			{Name: "learn"},
		},
	}
	if err := root.Run(t.Context(), []string{"sci"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := descendantPaths(root, "serach")
	if len(got) != 1 || got[0] != "sci zot search" {
		t.Errorf("descendantPaths(serach) = %v, want [sci zot search]", got)
	}
}

func TestDescendantPaths_EmptyWhenNothingMatches(t *testing.T) {
	t.Parallel()
	root := &cli.Command{
		Name:     "sci",
		Writer:   io.Discard,
		Commands: []*cli.Command{{Name: "db", Commands: []*cli.Command{{Name: "query"}}}},
	}
	if err := root.Run(t.Context(), []string{"sci"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := descendantPaths(root, "nope"); len(got) != 0 {
		t.Errorf("descendantPaths = %v, want none", got)
	}
}
