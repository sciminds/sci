package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"

	"github.com/sciminds/cli/internal/cmdutil"
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
	oaSyncKeys       []string
	oaSyncMissing    bool
	oaSyncEstimate   bool
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
			"$ sci zot --library all openalex sync --no-titles\n" +
			"$ sci zot --library all openalex sync --keys ABCD1234,EFGH5678\n" +
			"$ sci zot --library all openalex sync --missing --estimate --json\n\n" +
			"Writes openalex-works.ndjson plus a .meta.json sidecar, in the same\n" +
			"body-then-sidecar order as `zot export --format ndjson`.\n\n" +
			"Items with a DOI are looked up in batches of 50. Items without one\n" +
			"get a title search whose results are ALL kept as candidates — this\n" +
			"verb fetches, it never decides which candidate is the item. That\n" +
			"judgement is zot's, where the title normalization lives.\n\n" +
			"With no targeting flags this is a FULL REPLACE: every bibliographic\n" +
			"item, then every fetched work's references — about 4,200 metered\n" +
			"requests. --keys and --missing make it a targeted delta instead:\n" +
			"only those items are fetched, and the result is MERGED into the\n" +
			"cache already in staging. --estimate prices any of the three\n" +
			"without spending a request, which is what lets an unattended runner\n" +
			"decide before it bills.",
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
			// lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{
				Name:        "keys",
				Usage:       "fetch only these Zotero item keys and MERGE them into the existing cache (repeatable or comma-separated)",
				Destination: &oaSyncKeys,
			},
			&cli.BoolFlag{
				Name:        "missing",
				Usage:       "target every bibliographic item whose DOI the existing cache does not hold",
				Destination: &oaSyncMissing, Local: true,
			},
			&cli.BoolFlag{
				Name:        "estimate",
				Usage:       "print what the run would cost in requests and exit, without contacting OpenAlex",
				Destination: &oaSyncEstimate, Local: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keys := splitKeys(oaSyncKeys)
			delta := len(keys) > 0 || oaSyncMissing

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

			scope := "personal"
			if h := libraryHolderFromCtx(ctx); h != nil && h.Resolved != nil {
				scope = string(h.Resolved.Scope)
			}

			// A delta needs both bases BEFORE it fetches: --missing is
			// defined against the works cache, and refusing early costs
			// nothing while refusing late costs a fetch.
			var workBase, citedBase oacache.Base
			targets := targeting{Items: items}
			mode := "full"
			if delta {
				mode = "delta"
				if workBase, citedBase, err = loadDeltaBases(oaSyncDir()); err != nil {
					return err
				}
				targets = targetItems(items, keys, oaSyncMissing, workBase)
				if len(targets.Items) == 0 && len(targets.Unmatched) == len(keys) && len(keys) > 0 {
					return cmdutil.Coded(cmdutil.CodeNotFound,
						"no bibliographic item matches %s", strings.Join(targets.Unmatched, ", ")).
						WithTry("check the key with `sci zot read <key>` — annotations, notes and attachments are not works")
				}
			}

			want, scanned := wantFrom(targets.Items, oaSyncTitles)
			plan := oacache.Estimate(want)
			plan.Mode, plan.ItemsTargeted = mode, scanned

			if oaSyncEstimate {
				outputScoped(ctx, cmd, zot.OpenAlexEstimateResult{
					Scope: scope, Plan: plan, Keys: keys,
					KeysUnmatched: targets.Unmatched, MissingWithoutDOI: targets.WithoutDOI,
					MissingKnownAbsent: targets.KnownAbsent, StagingDir: oaSyncDir(),
				})
				return nil
			}

			result := zot.OpenAlexSyncResult{
				Scope: scope, Mode: mode, ItemsScanned: scanned,
				KeysUnmatched: targets.Unmatched, MissingWithoutDOI: targets.WithoutDOI,
				MissingKnownAbsent: targets.KnownAbsent,
			}
			// Nothing to fetch is an answer, not a failure — it is the
			// ordinary --missing outcome on a library that has not moved.
			// Merging zero records would rewrite the body for no reason and
			// restamp a sidecar that already told the truth.
			if delta && len(targets.Items) == 0 {
				result.OutPath = filepath.Join(oaSyncDir(), oacache.CacheFile)
				result.MetaPath = result.OutPath + ".meta.json"
				outputScoped(ctx, cmd, result)
				return nil
			}

			client, err := openalexClient()
			if err != nil {
				return err
			}
			res, err := oacache.Fetch(ctx, client, want,
				oacache.Options{TitleCandidates: oaSyncCandidates})
			if err != nil {
				return err
			}
			result.Stats, result.NotFound = res.Stats, res.NotFound

			// The reference title pool, from the referenced_works of the
			// works just fetched. Same pass, so the two files cannot
			// disagree about which works this library cites. A delta skips
			// every id either cache already holds — a new paper's
			// references are overwhelmingly works this corpus already
			// cites, and that is most of what makes a delta cheap.
			var known map[string]bool
			if delta {
				known = workBase.IDs()
				for id := range citedBase.IDs() {
					known[id] = true
				}
			}
			cited, err := oacache.FetchCited(ctx, client, res.Works, known, oacache.Options{})
			if err != nil {
				return err
			}
			result.CitedAsked, result.CitedGot = cited.Asked, len(cited.Works)

			if delta {
				d := oacache.Delta{
					Keys: keys, Missing: oaSyncMissing, ItemsTargeted: scanned,
					// The DOIs this run asked about, so the merge can keep
					// the not-found list a fact about the corpus rather
					// than about this run. See oacache.MergeNotFound.
					DOIsAsked: want.DOIs,
					Requests:  res.Stats.Requests + cited.Requests,
				}
				// Each write stamps the delta it was handed with the base
				// it merged over, so the two files get their own copies —
				// sharing one leaves the works result naming the title
				// pool's digest, which is a wrong answer that looks right.
				citedDelta := d
				body, merge, err := oacache.WriteDelta(oaSyncDir(), scope, res, workBase, &d)
				if err != nil {
					return err
				}
				citedBody, citedMerge, err := oacache.WriteCitedDelta(oaSyncDir(), scope, cited, citedBase, &citedDelta)
				if err != nil {
					return err
				}
				result.OutPath, result.MetaPath = body, body+".meta.json"
				result.CitedPath = citedBody
				result.Delta, result.Merged, result.CitedMerged = &d, &merge, &citedMerge
				outputScoped(ctx, cmd, result)
				return nil
			}

			body, err := oacache.Write(oaSyncDir(), scope, res)
			if err != nil {
				return err
			}
			citedBody, err := oacache.WriteCited(oaSyncDir(), scope, cited)
			if err != nil {
				return err
			}
			result.OutPath, result.MetaPath = body, body+".meta.json"
			result.CitedPath = citedBody
			outputScoped(ctx, cmd, result)
			return nil
		},
	}
}

// loadDeltaBases opens the two files a targeted sync merges into.
//
// Their absence is the one thing a delta cannot work around, and the
// failure it would otherwise produce is silent: a targeted run that wrote
// its four records into an empty staging directory produces
// openalex-works.ndjson with a valid sidecar, and the next build loads
// those four records as the whole OpenAlex corpus. So the refusal names
// the full sync, verbatim, as the way out.
func loadDeltaBases(dir string) (works, cited oacache.Base, err error) {
	fix := "sci zot --library all openalex sync"
	if oaSyncOut != "" {
		fix += " --out " + oaSyncOut
	}
	for _, name := range []string{oacache.CacheFile, oacache.CitedFile} {
		base, err := oacache.LoadBase(dir, name)
		if err != nil {
			if os.IsNotExist(err) {
				return works, cited, cmdutil.Coded(cmdutil.CodeNotFound,
					"there is no %s in %s to merge into — a targeted sync merges, it does not seed", name, dir).
					WithFix(fix)
			}
			return works, cited, err
		}
		if name == oacache.CacheFile {
			works = base
		} else {
			cited = base
		}
	}
	return works, cited, nil
}

// splitKeys flattens the --keys flag into item keys.
//
// Repeatable AND comma-separated, because both spellings arrive: a human
// types one --keys with a list, and a script loops. Keys are upper-cased
// because Zotero's are, and a lower-cased paste matching nothing would be
// reported as "no such item".
func splitKeys(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range raw {
		for part := range strings.SplitSeq(r, ",") {
			k := strings.ToUpper(strings.TrimSpace(part))
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// targeting is what a delta run's flags resolved to.
type targeting struct {
	// Items are the library items this run will look up.
	Items []local.Item
	// Unmatched are keys that named no bibliographic item — absent from the
	// library, or naming a note, annotation or attachment.
	Unmatched []string
	// WithoutDOI counts bibliographic items --missing could not judge
	// because they carry no DOI. See targetItems.
	WithoutDOI int
	// KnownAbsent counts items --missing skipped because a previous run
	// already asked OpenAlex for their DOI and got nothing. Reported, not
	// silent: it is the difference between "nothing new" and "44 papers
	// this index does not have".
	KnownAbsent int
}

// targetItems narrows the library to what a delta run should fetch.
//
// --keys names items outright. --missing asks a narrower question than it
// sounds like: which bibliographic items carry a DOI the works cache does
// not already hold. It deliberately does NOT try to answer that for an item
// with no DOI, because the only identifier such an item has is its title,
// and deciding whether a cached record IS that title is resolution —
// title_norm is defined exactly once, in zot, and a second definition here
// is the thing this package exists not to create. Those items are counted
// and reported instead: name one with --keys and it is looked up by title
// like any other.
func targetItems(items []local.Item, keys []string, missing bool, base oacache.Base) targeting {
	// Upper-cased here as well as in splitKeys: Zotero's keys are upper
	// case, and a key that matched nothing because it was pasted in lower
	// case would be reported as a paper the library does not hold.
	wanted := map[string]bool{}
	matched := map[string]bool{}
	for _, k := range keys {
		wanted[strings.ToUpper(strings.TrimSpace(k))] = true
	}

	var out targeting
	for i := range items {
		it := &items[i]
		switch it.Type {
		case "annotation", "note", "attachment":
			continue
		}
		key := strings.ToUpper(it.Key)
		named := wanted[key]
		if named {
			matched[key] = true
		}
		doi := strings.TrimSpace(it.DOI)
		// A DOI a previous run already asked about and OpenAlex did not
		// have is not "missing work to do" — it is a recorded answer. See
		// oacache.Base.KnownAbsent.
		absent := doi != "" && base.KnownAbsent(doi)
		uncached := missing && doi != "" && !base.HasDOI(doi) && !absent
		switch {
		case missing && doi == "":
			out.WithoutDOI++
		case missing && absent && !base.HasDOI(doi):
			out.KnownAbsent++
		}
		// Named and missing overlap; they select the same run, they do not
		// add up to two lookups for one item.
		if named || uncached {
			out.Items = append(out.Items, *it)
		}
	}
	out.Unmatched = lo.Reject(keys, func(k string, _ int) bool {
		return matched[strings.ToUpper(strings.TrimSpace(k))]
	})
	return out
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
