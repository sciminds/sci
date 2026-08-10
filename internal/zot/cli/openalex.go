package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/oacache"
	"github.com/urfave/cli/v3"
)

// openalex sync flag destinations.
var (
	oaSyncOut        string
	oaSyncCandidates int
	oaSyncTitles     bool
)

// openalexCommand groups the OpenAlex cache builders.
//
// This is a WRITE-TO-DISK verb, not a Zotero verb: it fetches from
// OpenAlex and writes a staging file. It lives here because sci already
// holds the OpenAlex credential and the retry-aware client, and because
// OpenAlex is now metered — an unauthenticated request draws on a small
// daily USD budget. The consumer (`zot`) is deployed to an internet-facing
// VM and should carry no billable key at all.
func openalexCommand() *cli.Command {
	return &cli.Command{
		Name:  "openalex",
		Usage: "Build the OpenAlex work cache that zot reads",
		Description: "$ sci zot --library all openalex sync\n\n" +
			"Fetches OpenAlex records for the library and writes them into zot's\n" +
			"staging directory. This is the one place the OpenAlex credential is\n" +
			"used on behalf of zot, which holds none of its own.",
		Commands: []*cli.Command{
			openalexSyncCommand(),
		},
	}
}

func openalexSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Fetch OpenAlex records for every library item into zot's staging",
		Description: "$ sci zot --library all openalex sync\n" +
			"$ sci zot --library all openalex sync --out ~/.local/share/zot/staging\n" +
			"$ sci zot --library all openalex sync --no-titles\n\n" +
			"Writes openalex-works.ndjson plus a .meta.json sidecar, in the same\n" +
			"body-then-sidecar order as `zot export --format ndjson`.\n\n" +
			"Items with a DOI are looked up in batches of 50. Items without one\n" +
			"get a title search whose results are ALL kept as candidates — this\n" +
			"verb fetches, it never decides which candidate is the item. That\n" +
			"judgement is zot's, where the title normalization lives.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "out", Aliases: []string{"o"},
				Usage:       "staging directory (default: $ZOT_STAGING or ~/.local/share/zot/staging)",
				Sources:     cli.EnvVars("ZOT_STAGING"),
				Destination: &oaSyncOut, Local: true,
			},
			&cli.IntFlag{
				Name: "candidates", Value: oacache.DefaultTitleCandidates,
				Usage:       "results to keep per title lookup",
				Destination: &oaSyncCandidates, Local: true,
			},
			&cli.BoolFlag{
				Name: "titles", Value: true,
				Usage:       "look up items that have no DOI by title",
				Destination: &oaSyncTitles, Local: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// --library all, same as the ndjson dump: the cache serves a
			// corpus that spans both libraries, and a per-library cache
			// would have to be merged by the consumer.
			_, db, err := openLocalDBAllowAll(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.ListAll(local.ListFilter{})
			if err != nil {
				return err
			}
			want, scanned := wantFrom(items, oaSyncTitles)

			client, err := openalexClient()
			if err != nil {
				return err
			}
			res, err := oacache.Fetch(ctx, client, want,
				oacache.Options{TitleCandidates: oaSyncCandidates})
			if err != nil {
				return err
			}

			scope := "personal"
			if h := libraryHolderFromCtx(ctx); h != nil && h.Resolved != nil {
				scope = string(h.Resolved.Scope)
			}
			body, err := oacache.Write(oaSyncDir(), scope, res)
			if err != nil {
				return err
			}

			// The reference title pool, from the referenced_works of the
			// works just fetched. Same pass, so the two files cannot
			// disagree about which works this library cites.
			cited, err := oacache.FetchCited(ctx, client, res.Works, oacache.Options{})
			if err != nil {
				return err
			}
			citedBody, err := oacache.WriteCited(oaSyncDir(), scope, cited)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.OpenAlexSyncResult{
				Scope: scope, OutPath: body, MetaPath: body + ".meta.json",
				ItemsScanned: scanned, Stats: res.Stats, NotFound: res.NotFound,
				CitedPath: citedBody, CitedAsked: cited.Asked, CitedGot: len(cited.Works),
			})
			return nil
		},
	}
}

// oaSyncDir resolves where the cache lands. It mirrors zot's own default
// so the common case needs no flag, and it is deliberately NOT sci's data
// dir: this file is an input to another tool, not sci's own state.
func oaSyncDir() string {
	if oaSyncOut != "" {
		return oaSyncOut
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "staging"
	}
	return filepath.Join(home, ".local", "share", "zot", "staging")
}

// wantFrom turns library items into a lookup plan.
//
// Only bibliographic items are worth looking up — annotations, notes and
// attachments are Zotero objects, not works, and a title search for one
// would burn a request and return noise. An item with a DOI is never also
// looked up by title: the DOI is the stronger identifier and the second
// request would buy nothing.
func wantFrom(items []local.Item, byTitle bool) (oacache.Want, int) {
	var w oacache.Want
	var scanned int
	seenDOI, seenTitle := map[string]bool{}, map[string]bool{}

	for i := range items {
		it := &items[i]
		switch it.Type {
		case "annotation", "note", "attachment":
			continue
		}
		scanned++

		if doi := strings.TrimSpace(it.DOI); doi != "" {
			k := strings.ToLower(doi)
			if !seenDOI[k] {
				seenDOI[k] = true
				w.DOIs = append(w.DOIs, doi)
			}
			// Remember the title against the DOI. It costs nothing unless
			// OpenAlex turns out not to hold that DOI, and then it is the
			// difference between a resolved item and a local mint.
			if t := strings.TrimSpace(it.Title); t != "" {
				if w.FallbackTitles == nil {
					w.FallbackTitles = map[string]string{}
				}
				if _, ok := w.FallbackTitles[doi]; !ok {
					w.FallbackTitles[doi] = t
				}
			}
			continue
		}
		// Cross-library duplicates share a title, so deduplicating here is
		// what keeps 189 duplicate pairs from costing 189 extra requests.
		if byTitle {
			t := strings.TrimSpace(it.Title)
			if t != "" && !seenTitle[strings.ToLower(t)] {
				seenTitle[strings.ToLower(t)] = true
				w.Titles = append(w.Titles, t)
			}
		}
	}
	return w, scanned
}
