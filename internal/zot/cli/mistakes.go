package cli

// mistakes.go is the zot LLM-mistake corpus: the helpers that turn the
// failures agents predictably hit (missing --library, typo'd collection
// names, unsynced keys, no config) into CodedErrors carrying a resubmittable
// Fix or an actionable Try. The rule, borrowed from the dabble/gundam evals:
// a common mistake that dead-ends with neither is a bug — agents don't
// infer their way out, they thrash. mistakes_test.go is the audit gate.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/hygiene"
	"github.com/sciminds/sci/pkg/local"
)

// insertLibraryFix rebuilds the user's command line with `--library personal`
// inserted right after the zot token, quoting arguments that need it. Returns
// "" when argv has no zot token (nothing sensible to rebuild) — callers fall
// back to Try-only guidance.
func insertLibraryFix(argv []string) string {
	if len(argv) < 2 {
		return ""
	}
	zotIdx := lo.IndexOf(argv[1:], "zot")
	if zotIdx < 0 {
		return ""
	}
	zotIdx++ // shift past argv[0]

	parts := []string{"sci"}
	for i, arg := range argv[1:] {
		parts = append(parts, shellQuote(arg))
		if i+1 == zotIdx {
			parts = append(parts, "--library", "personal")
		}
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps an argument in single quotes when it contains characters
// that would split or expand in a shell. Fix strings must resubmit verbatim.
func shellQuote(arg string) string {
	if arg == "" || strings.ContainsAny(arg, " \t\"'$&|;<>()*?#~") {
		return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return arg
}

// errLibraryRequiredArgs is the testable core of errLibraryRequired: argv is
// the command line to rebuild into the Fix.
func errLibraryRequiredArgs(reason string, argv []string) error {
	coded := cmdutil.Coded(cmdutil.CodeUsage,
		"--library is required (values: personal, shared) — both libraries are configured; %s", reason).
		WithTry("add --library personal (or shared for the group library) anywhere in the command; full agent cheat sheet: sci zot guide --json")
	if fix := insertLibraryFix(argv); fix != "" {
		coded = coded.WithFix(fix)
	}
	return coded
}

// requireConfigCoded wraps zot.RequireConfig, upgrading the not-configured
// case to a CodedError with the setup command as the fix.
func requireConfigCoded() (*zot.Config, error) {
	cfg, err := zot.RequireConfig()
	if err != nil {
		if errors.Is(err, zot.ErrNotConfigured) {
			return nil, cmdutil.Coded(cmdutil.CodeNotConfigured, "%v", err).
				WithFix("sci zot setup").
				WithTry("after setup, run 'sci zot guide --json' for the agent cheat sheet")
		}
		return nil, err
	}
	return cfg, nil
}

// suggestCollections returns up to three collection names similar to input,
// best match first. The 0.5 similarity floor keeps garbage out of the nudge.
func suggestCollections(input string, cols []local.Collection) []string {
	const floor = 0.5
	type scored struct {
		name  string
		ratio float64
	}
	ranked := lo.FilterMap(cols, func(c local.Collection, _ int) (scored, bool) {
		r := hygiene.SimilarityRatio(strings.ToLower(input), strings.ToLower(c.Name))
		return scored{name: c.Name, ratio: r}, r >= floor
	})
	slices.SortFunc(ranked, func(a, b scored) int { return cmp.Compare(b.ratio, a.ratio) })
	names := lo.Map(ranked, func(s scored, _ int) string { return s.name })
	if len(names) > 3 {
		names = names[:3]
	}
	return names
}

// resolveCiteKeyArg best-effort resolves a non-key positional as a cite key
// against the local library. Returns the resolved 8-char item key, or "" when
// resolution isn't unambiguous (never guess — the caller falls through to a
// direct read and its coded not-found error).
func resolveCiteKeyArg(ctx context.Context, arg string) string {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = db.Close() }()

	hits, err := db.SearchWith("@citekey:"+arg, 10, local.SearchOptions{})
	if err != nil {
		return ""
	}
	exact := lo.Filter(hits, func(it local.Item, _ int) bool {
		// Stored key match, or a drifted synthesized key resolving via its
		// -ZOTKEY suffix (same convention as zot bib / export round-trips).
		return strings.EqualFold(it.Citekey, arg) || strings.HasSuffix(arg, "-"+it.Key)
	})
	if len(exact) == 1 {
		return exact[0].Key
	}
	return ""
}

// itemNotFoundErr wraps a local read miss with the two escape hatches agents
// need: resubmit with --remote (sync lag — items just created via the API
// aren't in the local mirror yet), or search for the right key.
func itemNotFoundErr(ctx context.Context, key string, err error) error {
	scope := scopeFromCtx(ctx)
	return cmdutil.Coded(cmdutil.CodeNotFound, "%v", err).
		WithFix(fmt.Sprintf("sci zot --library %s item read %s --remote", scope, key)).
		WithTry(fmt.Sprintf("if the key came from a search elsewhere, re-find it: sci zot --library %s search <title words> (cite keys also work as the positional)", scope))
}

// itemsNotFoundErr is itemNotFoundErr's batch sibling: a multi-key read
// with any missing key fails whole, naming every straggler — a partial
// result would be the same silent drop the multi-key form exists to fix.
// The Fix resubmits ALL requested keys with --remote, not just the missing
// ones, so the agent gets one coherent result instead of stitching two.
func itemsNotFoundErr(ctx context.Context, requested, missing []string) error {
	scope := scopeFromCtx(ctx)
	return cmdutil.Coded(cmdutil.CodeNotFound, "item(s) not found: %s", strings.Join(missing, ", ")).
		WithFix(fmt.Sprintf("sci zot --library %s item read %s --remote", scope, strings.Join(requested, " "))).
		WithTry(fmt.Sprintf("if a key came from a search elsewhere, re-find it: sci zot --library %s search <title words> (cite keys also work as positionals)", scope))
}

// notFoundCollectionErr builds the coded not-found error for a collection
// name, with a did-you-mean Try when the library has something close.
func notFoundCollectionErr(input string, cols []local.Collection) error {
	coded := cmdutil.Coded(cmdutil.CodeNotFound, "collection %q not found", input)
	if suggestions := suggestCollections(input, cols); len(suggestions) > 0 {
		quoted := lo.Map(suggestions, func(s string, _ int) string { return fmt.Sprintf("%q", s) })
		return coded.WithTry(fmt.Sprintf("similar collections exist: %s — use one of those names, or the 8-char key from 'sci zot collection list'", strings.Join(quoted, ", ")))
	}
	return coded.WithTry("run 'sci zot collection list' to see available names and keys")
}
