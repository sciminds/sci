package cli

import (
	"bufio"
	"context"
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
// The collection path uses the local SQLite (fast, but stale if Zotero
// desktop hasn't finished syncing). The saved-search and keys-from paths
// hit the Zotero Web API so they always reflect the live server state.
func resolvePDFItemSource(ctx context.Context, cmd *cli.Command) ([]local.Item, string, func(), error) {
	switch {
	case pdfsSavedSearch != "":
		items, label, err := loadFromSavedSearch(ctx, pdfsSavedSearch)
		return items, label, nil, err

	case pdfsKeysFrom != "":
		items, label, err := loadFromKeysFile(ctx, pdfsKeysFrom)
		return items, label, nil, err

	default:
		// --collection (or default 'missing-pdf'). Local-DB path.
		_, db, err := openLocalDB(ctx)
		if err != nil {
			return nil, "", nil, err
		}
		closer := func() { _ = db.Close() }
		name := pdfsCollection
		if name == "" {
			name = defaultPDFCollection
		}
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
	search, err := resolveSavedSearch(ctx, c, ref)
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
	label := fmt.Sprintf("saved-search:%s", search.Data.Name)
	return items, label, nil
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

// resolveSavedSearch fetches a saved search by 8-char key or by exact name.
// Names are looked up via the live ListSavedSearches call so we don't
// depend on local SQLite here either — keeps the source uniformly fresh.
func resolveSavedSearch(ctx context.Context, c *api.Client, ref string) (*client.Search, error) {
	if isZoteroKey(ref) {
		s, err := c.GetSavedSearch(ctx, ref)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	all, err := c.ListSavedSearches(ctx)
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	matches := lo.Filter(all, func(s client.Search, _ int) bool {
		return s.Data.Name == ref
	})
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no saved search named %q", ref)
	case 1:
		return &matches[0], nil
	default:
		keys := lo.Map(matches, func(s client.Search, _ int) string { return s.Key })
		return nil, fmt.Errorf("saved-search name %q is ambiguous, matches keys: %s", ref, strings.Join(keys, ", "))
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
