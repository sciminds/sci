package cli

import (
	"context"
	"time"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/xrcache"
	"github.com/sciminds/cli/pkg/crossref"
	"github.com/urfave/cli/v3"
)

// crossref sync flag destinations.
var (
	xrSyncOut     string
	xrSyncRows    int
	xrSyncRetries int
	xrSyncPauseMS int
)

// crossrefCommand groups the Crossref sweep.
//
// Like `openalex`, this is a WRITE-TO-DISK verb rather than a Zotero one:
// it fetches from a bibliographic index and writes a staging file that zot
// reads. It lives in sci because sci is where network access lives — zot
// holds no credentials and makes no external calls, anywhere.
//
// Unlike OpenAlex, Crossref is free and unmetered, so this sweep can be
// re-run without a budget conversation.
func crossrefCommand() *cli.Command {
	return &cli.Command{
		Name:  "crossref",
		Usage: "Build the Crossref candidate cache that zot cross-checks against",
		Description: "$ sci zot --library all crossref sync\n\n" +
			"Asks Crossref for every DOI-less title in the library and writes the\n" +
			"candidates into zot's staging directory, as a SECOND opinion on the\n" +
			"DOIs OpenAlex inferred from the same titles.",
		Commands: []*cli.Command{
			crossrefSyncCommand(),
		},
	}
}

func crossrefSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Sweep Crossref for every DOI-less title into zot's staging",
		Description: "$ sci zot --library all crossref sync\n" +
			"$ sci zot --library all crossref sync --rows 10\n\n" +
			"Writes crossref-candidates.ndjson plus a .meta.json sidecar, in the\n" +
			"same body-then-sidecar order as every other staged input.\n\n" +
			"Why a second index: zot resolves a DOI-less item by matching its\n" +
			"title against OpenAlex, and accepts that at 'medium' confidence.\n" +
			"Writing such a DOI back into Zotero would make the item resolve at\n" +
			"'high' next build — by a DOI zot itself guessed. A DOI two\n" +
			"independent indexes reach from the same title is evidence; one\n" +
			"index answering alone is an inference.\n\n" +
			"Every candidate is kept, tagged with the title that found it. This\n" +
			"verb never decides which candidate IS the item: Crossref's ranking\n" +
			"is fuzzy enough that its top hit for a real 1962 paper can be a\n" +
			"differently-named dataset. That judgement is zot's, where the title\n" +
			"normalization is defined.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "out", Aliases: []string{"o"},
				Usage:       "staging directory (default: $ZOT_STAGING or ~/.local/share/zot/staging)",
				Sources:     cli.EnvVars("ZOT_STAGING"),
				Destination: &xrSyncOut, Local: true,
			},
			&cli.IntFlag{
				Name: "rows", Value: crossref.DefaultRows,
				Usage:       "candidates to keep per title",
				Destination: &xrSyncRows, Local: true,
			},
			&cli.IntFlag{
				Name: "retries", Value: 2,
				Usage:       "extra attempts before a title is reported unaskable",
				Destination: &xrSyncRetries, Local: true,
			},
			&cli.IntFlag{
				Name: "pause-ms", Value: 100,
				Usage:       "delay between requests, for the polite pool",
				Destination: &xrSyncPauseMS, Local: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// --library all, for the same reason the OpenAlex cache spans
			// both: the corpus does, and a per-library cache would have to
			// be merged by the consumer.
			_, db, err := openLocalDBAllowAll(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.ListAll(local.ListFilter{})
			if err != nil {
				return err
			}
			// The same plan the OpenAlex title path uses: bibliographic
			// items that have no DOI. An item WITH a DOI needs no second
			// opinion — nothing was inferred about it.
			want, _ := wantFrom(items, true)

			cfg, err := zot.LoadConfig()
			if err != nil {
				return err
			}
			mailto := ""
			if cfg != nil {
				mailto = cfg.OpenAlexEmail
			}

			res, err := xrcache.Fetch(ctx, crossref.New(mailto), want.Titles, xrcache.Options{
				Rows:    xrSyncRows,
				Retries: xrSyncRetries,
				Pause:   time.Duration(xrSyncPauseMS) * time.Millisecond,
			})
			if err != nil {
				return err
			}

			scope := "personal"
			if h := libraryHolderFromCtx(ctx); h != nil && h.Resolved != nil {
				scope = string(h.Resolved.Scope)
			}
			body, err := xrcache.Write(xrSyncDir(), scope, res)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.CrossrefSyncResult{
				Scope: scope, OutPath: body, MetaPath: body + ".meta.json",
				TitlesSeen: len(want.Titles), Stats: res.Stats, Errored: res.Errored,
			})
			return nil
		},
	}
}

// xrSyncDir resolves where the cache lands, mirroring oaSyncDir: zot's own
// default, so the common case needs no flag, and deliberately not sci's
// data dir — this file is an input to another tool, not sci's own state.
func xrSyncDir() string {
	if xrSyncOut != "" {
		return xrSyncOut
	}
	return oaSyncDir()
}
