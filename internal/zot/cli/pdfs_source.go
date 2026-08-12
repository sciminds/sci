package cli

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/savedsearch"
	"github.com/urfave/cli/v3"
)

// validatePDFSourceFlags rejects invalid combinations of --collection,
// --saved-search, --keys-from. At most one may be set.
func validatePDFSourceFlags(cmd *cli.Command) error {
	set := 0
	if pdfsCollection != "" {
		set++
	}
	if pdfsSavedSearch != "" {
		set++
	}
	if pdfsKeysFrom != "" {
		set++
	}
	if set > 1 {
		return cmdutil.UsageErrorf(cmd, "--collection, --saved-search, --keys-from are mutually exclusive")
	}
	return nil
}

// resolvePDFItemSource picks the right item source based on which of the
// three source flags was set. Returns the resolved items, a human-readable
// label for the source (used as `Collection` in the result struct so JSON
// consumers can see what was scanned), and an optional cleanup closer.
//
// Collections and saved searches are one usability surface: a *name* given
// to either --collection or --saved-search (or the no-flag default) resolves
// against both kinds — the flag's own kind first, the other as fallback — so
// users never have to remember which one they made in Zotero. 8-char keys
// stay kind-specific (no fallback). The collection path reads local SQLite
// (fast, but stale if Zotero desktop hasn't finished syncing); the
// saved-search and keys-from paths hit the Zotero Web API so they always
// reflect the live server state.
func resolvePDFItemSource(ctx context.Context) ([]local.Item, string, func(), error) {
	loaders := pdfSourceLoaders{
		collection: func(name string) ([]local.Item, string, func(), error) {
			return loadFromCollection(ctx, name)
		},
		savedSearch: func(name string) ([]local.Item, string, error) {
			return loadFromSavedSearch(ctx, name)
		},
	}
	switch {
	case pdfsSavedSearch != "":
		return loadEitherSource(pdfsSavedSearch, false, loaders)

	case pdfsKeysFrom != "":
		items, label, err := loadFromKeysFile(ctx, pdfsKeysFrom)
		return items, label, nil, err

	default:
		return loadEitherSource(cmp.Or(pdfsCollection, defaultPDFCollection), true, loaders)
	}
}

// pdfSourceLoaders bundles the two name-addressable item sources so
// loadEitherSource's fallback logic is testable with stubs.
type pdfSourceLoaders struct {
	collection  func(name string) ([]local.Item, string, func(), error)
	savedSearch func(name string) ([]local.Item, string, error)
}

// loadEitherSource resolves ref against both name-addressable sources:
// the preferred kind first, then the other when the name isn't found there.
// Key-shaped refs never fall back — an 8-char key names one object in one
// namespace, and "not found" for a key is an answer, not a routing miss.
// A name missing from both kinds returns one combined not-found error.
func loadEitherSource(ref string, collectionFirst bool, l pdfSourceLoaders) ([]local.Item, string, func(), error) {
	fromCollection := func() ([]local.Item, string, func(), error) { return l.collection(ref) }
	fromSavedSearch := func() ([]local.Item, string, func(), error) {
		items, label, err := l.savedSearch(ref)
		return items, label, nil, err
	}
	first, second := fromCollection, fromSavedSearch
	if !collectionFirst {
		first, second = fromSavedSearch, fromCollection
	}

	items, label, closer, err := first()
	if err == nil {
		return items, label, closer, nil
	}
	if isZoteroKey(ref) || !sourceNotFound(err) {
		return nil, "", nil, err
	}
	items, label, closer, err2 := second()
	if err2 == nil {
		return items, label, closer, nil
	}
	if !sourceNotFound(err2) {
		return nil, "", nil, err2
	}
	return nil, "", nil, cmdutil.Coded(cmdutil.CodeNotFound,
		"no collection or saved search named %q", ref).
		WithTry("run 'sci zot collection list' and 'sci zot saved-search list' to see available names and keys")
}

// sourceNotFound reports whether err means "no such collection / saved
// search" — the only condition that licenses falling back to the other
// source kind. Transport, ambiguity, and translation errors all propagate.
func sourceNotFound(err error) bool {
	if errors.Is(err, api.ErrNotFound) {
		return true
	}
	ce, ok := errors.AsType[*cmdutil.CodedError](err)
	return ok && ce.Code == cmdutil.CodeNotFound
}

// loadFromCollection is the local-SQLite collection path: resolve the
// name-or-key, list the collection's items. The returned closer owns the
// DB handle and is non-nil only on success.
func loadFromCollection(ctx context.Context, name string) ([]local.Item, string, func(), error) {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	closer := func() { _ = db.Close() }
	collKey, resolvedName, err := resolveCollectionKey(db, name)
	if err != nil {
		closer()
		return nil, "", nil, err
	}
	items, err := db.ListAll(local.ListFilter{CollectionKey: collKey})
	if err != nil {
		closer()
		return nil, "", nil, fmt.Errorf("list items in %q: %w", resolvedName, err)
	}
	return items, resolvedName, closer, nil
}

// loadFromSavedSearch resolves the saved search by key or name, translates
// its conditions into Zotero Web API filter params, and lists matching
// items live. Errors with the offending clauses listed when the saved
// search uses conditions outside the translatable set — silently dropping
// them would produce results that don't match what desktop renders.
func loadFromSavedSearch(ctx context.Context, ref string) ([]local.Item, string, error) {
	c, err := requireAPIClient(ctx)
	if err != nil {
		return nil, "", err
	}
	search, err := c.ResolveSavedSearch(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	filters, unsupported := savedsearch.Translate(search.Data.Conditions)
	if len(unsupported) > 0 {
		lines := lo.Map(unsupported, func(u savedsearch.Unsupported, _ int) string {
			return "  - " + u.String()
		})
		return nil, "", fmt.Errorf(
			"saved search %q has %d condition(s) the Zotero Web API can't express:\n%s\nuse --keys-from with a key list exported from Zotero desktop instead",
			search.Data.Name, len(unsupported), strings.Join(lines, "\n"),
		)
	}
	clientItems, err := c.ListItems(ctx, api.ListItemsOptions{
		CollectionKey: filters.CollectionKey,
		ItemType:      itemTypeFilterFromSavedSearch(filters),
		Tag:           tagFilterFromSavedSearch(filters),
		Top:           filters.TopOnly,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list items via saved search %q: %w", search.Data.Name, err)
	}
	items := lo.Map(clientItems, func(it client.Item, _ int) local.Item {
		return api.ItemFromClient(&it)
	})
	return items, fmt.Sprintf("saved-search:%s", search.Data.Name), nil
}

// itemTypeFilterFromSavedSearch combines the positive + negated itemType
// filters into the single string the Zotero `?itemType=` parameter accepts.
// Both filters can co-exist via the `||`/`-` grammar.
func itemTypeFilterFromSavedSearch(f savedsearch.APIFilters) string {
	switch {
	case f.ItemType != "" && f.NotItemType != "":
		return f.ItemType + " || -" + f.NotItemType
	case f.ItemType != "":
		return f.ItemType
	case f.NotItemType != "":
		return "-" + f.NotItemType
	default:
		return ""
	}
}

// tagFilterFromSavedSearch combines positive + negated tag filters into
// the single string `?tag=` accepts. The Zotero API allows repeated `tag=`
// query params for AND-ing multiple positive filters; our generated
// wrapper sends just one, which is fine for the saved-search translator's
// at-most-one-positive + at-most-one-negated invariant.
func tagFilterFromSavedSearch(f savedsearch.APIFilters) string {
	switch {
	case f.Tag != "" && f.NotTag != "":
		return f.Tag + " || -" + f.NotTag
	case f.Tag != "":
		return f.Tag
	case f.NotTag != "":
		return "-" + f.NotTag
	default:
		return ""
	}
}

// isZoteroKey reports whether ref looks like an 8-char Zotero object key
// (uppercase letters + digits). Keys are 8 chars exactly; names are
// almost always longer or contain mixed case / spaces.
func isZoteroKey(ref string) bool {
	if len(ref) != 8 {
		return false
	}
	for _, r := range ref {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// loadFromKeysFile reads item keys from path (or stdin when path == "-")
// and resolves them against the Zotero Web API in batches of 50 (the
// `?itemKey=` cap).
func loadFromKeysFile(ctx context.Context, path string) ([]local.Item, string, error) {
	keys, err := readItemKeys(path)
	if err != nil {
		return nil, "", err
	}
	if len(keys) == 0 {
		return nil, "", fmt.Errorf("no item keys found in %s", describeKeySource(path))
	}
	c, err := requireAPIClient(ctx)
	if err != nil {
		return nil, "", err
	}
	const itemKeyBatch = 50
	var clientItems []client.Item
	for _, chunk := range lo.Chunk(keys, itemKeyBatch) {
		page, err := c.ListItems(ctx, api.ListItemsOptions{ItemKeys: chunk})
		if err != nil {
			return nil, "", fmt.Errorf("list items by key: %w", err)
		}
		clientItems = append(clientItems, page...)
	}
	items := lo.Map(clientItems, func(it client.Item, _ int) local.Item {
		return api.ItemFromClient(&it)
	})
	label := fmt.Sprintf("keys-from:%s (%d keys)", describeKeySource(path), len(keys))
	return items, label, nil
}

// readItemKeys parses one-per-line keys from path (or stdin if path is "-").
// Skips blank lines and lines starting with #. Validates each remaining
// token is an 8-char Zotero key — anything else is rejected with the
// offending line number so the user can fix the input quickly.
func readItemKeys(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open keys file: %w", err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	var keys []string
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if !isZoteroKey(raw) {
			return nil, fmt.Errorf("line %d: %q is not an 8-char Zotero item key", line, raw)
		}
		keys = append(keys, raw)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read keys: %w", err)
	}
	return lo.Uniq(keys), nil
}

// describeKeySource returns a short label for the source of item keys, used
// in the result's Collection label and in error messages.
func describeKeySource(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}
