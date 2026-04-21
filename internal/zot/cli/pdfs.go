package cli

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/pdffind"
	"github.com/urfave/cli/v3"
)

// pdfs flag destinations.
var (
	pdfsCollection string
	pdfsDownload   string
	pdfsLimit      int
)

// defaultPDFCollection is the collection name we assume when --collection is
// not passed. Matches the convention in CLAUDE.md and the user's library.
const defaultPDFCollection = "missing-pdf"

func pdfsCommand() *cli.Command {
	return &cli.Command{
		Name:  "pdfs",
		Usage: "Find retrievable PDFs on OpenAlex for items in a collection",
		Description: `$ zot --library personal doctor pdfs                          # scans default 'missing-pdf' collection
$ zot --library personal doctor pdfs --collection ABCD1234    # by key
$ zot --library personal doctor pdfs --collection missing-pdf # by name
$ zot --library personal doctor pdfs --download ~/pdfs        # also retrieve each PDF
$ zot --library personal doctor pdfs --json > missing.json

For each item in the target collection, queries OpenAlex:
  - by DOI if present (deterministic),
  - else by title (top search hit, flagged as 'title-match').

Reports what OpenAlex has that Zotero doesn't: PDF URL, landing-page
URL, DOI, open-access status. With --download, each finding's PDFURL
is fetched into DIR as <itemKey>.pdf. HTTP errors and non-PDF
content-types (paywall HTML) are recorded per-item; the batch continues.

No Zotero writes. Attaching downloaded PDFs as Zotero child
attachments is a separate command (coming later).`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "collection",
				Aliases:     []string{"c"},
				Usage:       "collection name or key (default: missing-pdf)",
				Destination: &pdfsCollection,
				Local:       true,
			},
			&cli.StringFlag{
				Name:        "download",
				Aliases:     []string{"d"},
				Usage:       "retrieve each PDF to DIR (skipped if empty)",
				Destination: &pdfsDownload,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all; does not cap the scan)",
				Destination: &pdfsLimit,
				Local:       true,
			},
		},
		Action: runPDFs,
	}
}

func runPDFs(ctx context.Context, cmd *cli.Command) error {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	name := pdfsCollection
	if name == "" {
		name = defaultPDFCollection
	}
	collKey, resolvedName, err := resolveCollectionKey(db, name)
	if err != nil {
		return err
	}

	items, err := db.ListAll(local.ListFilter{CollectionKey: collKey})
	if err != nil {
		return fmt.Errorf("list items in %q: %w", resolvedName, err)
	}

	oa, err := openalexClient()
	if err != nil {
		return err
	}

	res, err := pdffind.Scan(ctx, items, oa)
	if err != nil {
		return err
	}

	out := pdffind.CLIResult{
		Collection: resolvedName,
		Scanned:    res.Scanned,
		Findings:   res.Findings,
		Limit:      pdfsLimit,
	}

	if pdfsDownload != "" && len(res.Findings) > 0 {
		// Give PDFs a reasonable ceiling — they're usually a few MB but some
		// are 100+. 5 minutes accommodates the largest without wedging the CLI.
		httpClient := &http.Client{Timeout: 5 * time.Minute}
		fresh, derr := pdffind.Download(ctx, httpClient, res.Findings, pdfsDownload)
		if derr != nil {
			return derr
		}
		out.Findings = fresh
		out.Downloaded = true
	}

	cmdutil.Output(cmd, out)
	return nil
}

// zoteroKeyRE matches an 8-character Zotero-style key. Used to decide whether
// --collection should be looked up as a name or used verbatim as a key.
var zoteroKeyRE = regexp.MustCompile(`^[A-Z0-9]{8}$`)

// resolveCollectionKey accepts either a Zotero collection key or a collection
// name (case-insensitive) and returns (key, displayName, err).
//
// Name collisions (two collections with the same name, e.g. nested) are
// flagged as an error with both keys listed so the user can disambiguate.
func resolveCollectionKey(db local.Reader, input string) (key, displayName string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("collection is required")
	}
	if zoteroKeyRE.MatchString(input) {
		// Treat as key; look up the name for a friendlier display, but don't
		// fail if the key isn't found locally — let downstream ListAll surface
		// an empty result set if the user typo'd.
		cols, lerr := db.ListCollections()
		if lerr != nil {
			return input, input, nil // best-effort; return the key as display.
		}
		if c, ok := lo.Find(cols, func(c local.Collection) bool { return c.Key == input }); ok {
			return c.Key, c.Name, nil
		}
		return input, input, nil
	}

	cols, lerr := db.ListCollections()
	if lerr != nil {
		return "", "", fmt.Errorf("list collections: %w", lerr)
	}
	matches := lo.Filter(cols, func(c local.Collection, _ int) bool {
		return strings.EqualFold(c.Name, input)
	})
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("collection %q not found (use 'zot collection list' to see names)", input)
	case 1:
		return matches[0].Key, matches[0].Name, nil
	default:
		keys := lo.Map(matches, func(c local.Collection, _ int) string { return c.Key })
		return "", "", fmt.Errorf("collection name %q is ambiguous — multiple matches: %s (pass --collection <key> instead)", input, strings.Join(keys, ", "))
	}
}
