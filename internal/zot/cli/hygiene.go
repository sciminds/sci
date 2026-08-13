package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/hygiene"
	"github.com/urfave/cli/v3"
)

// Flag destinations for hygiene commands.
var (
	missingFields string
	missingLimit  int

	dupStrategy  string
	dupFuzzy     bool
	dupThreshold float64
	dupLimit     int

	invalidFields string
	invalidLimit  int

	orphansKinds      string
	orphansLimit      int
	orphansCheckFiles bool

	citekeysLimit int
)

func missingCommand() *cli.Command {
	return &cli.Command{
		Name:  "missing",
		Usage: "Scan the library for items missing common fields",
		Description: `$ sci zot doctor missing
$ sci zot doctor missing --field title,creators
$ sci zot doctor missing --field doi,abstract
$ sci zot doctor missing --limit 0 --json > coverage.json

Fields: title, creators, date, doi, abstract, url, pdf, tags. Defaults to all.
Severity: title=error, creators/date=warn, others=info.

This is a report over the local database. Filling the gaps from an upstream
index is a metered lookup plus a Zotero write, and lives in the zot binary
as ` + "`zot enrich`" + `, which fills from data already on disk.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "field",
				Aliases:     []string{"f"},
				Usage:       "comma-separated fields to check (title,creators,date,doi,abstract,url,pdf,tags)",
				Destination: &missingFields,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all)",
				Destination: &missingLimit,
				Local:       true,
			},
		},
		Action: runMissing,
	}
}

func runMissing(ctx context.Context, cmd *cli.Command) error {
	fields, err := parseMissingFieldList(missingFields)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%s", err.Error())
	}
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	rep, err := hygiene.Missing(db, fields)
	if err != nil {
		return err
	}

	outputScoped(ctx, cmd, zot.MissingResult{Report: rep, Limit: missingLimit})
	return nil
}

func duplicatesCommand() *cli.Command {
	return &cli.Command{
		Name:  "duplicates",
		Usage: "Find potential duplicate items (by DOI, title, or PDF bytes)",
		Description: `$ sci zot doctor duplicates                  # fast: DOI + exact-normalized title
$ sci zot doctor duplicates --fuzzy          # adds slow fuzzy title pass (~30s on 5k items)
$ sci zot doctor duplicates --strategy doi
$ sci zot doctor duplicates --strategy content   # same PDF bytes, whatever the metadata says
$ sci zot doctor duplicates --fuzzy --threshold 0.9
$ sci zot doctor duplicates --limit 0 --json > dupes.json

Strategies: doi (strongest), title, both (default), content.

Fast mode catches shared DOIs and titles that are identical after
normalization (case/whitespace/punctuation). Use --fuzzy to additionally
pair near-identical titles (typos, punctuation drift) using Levenshtein
similarity — accurate but O(n²) on the title-only singletons.

DOI matches subsume title matches when both fire on the same items.

--strategy content ignores metadata entirely and clusters items whose
PDFs are byte-identical, which is the only way to catch the same scan
filed twice under different titles and no DOI. It runs alone (byte
identity needs no corroboration) and hashes every PDF in the library,
so it costs tens of seconds where the metadata passes cost none.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "strategy",
				Aliases:     []string{"s"},
				Value:       "both",
				Usage:       "match strategy: doi, title, both, content",
				Destination: &dupStrategy,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "fuzzy",
				Usage:       "enable slow fuzzy title matching (Levenshtein)",
				Destination: &dupFuzzy,
				Local:       true,
			},
			&cli.FloatFlag{
				Name:        "threshold",
				Aliases:     []string{"t"},
				Value:       0.85,
				Usage:       "fuzzy title similarity floor (0..1, requires --fuzzy)",
				Destination: &dupThreshold,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       20,
				Usage:       "max clusters to print (0 = all)",
				Destination: &dupLimit,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			strategy, err := parseStrategy(dupStrategy)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			if dupThreshold < 0 || dupThreshold > 1 {
				return cmdutil.UsageErrorf(cmd, "--threshold must be in [0, 1]")
			}
			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			// Content clustering needs the files themselves, which no
			// metadata pass touches — hash them before handing the pure
			// clusterer a finished map.
			var contentKeys map[string]string
			if strategy == hygiene.StrategyContent {
				contentKeys, err = hashLibraryPDFs(ctx, db, cfg.DataDir)
				if err != nil {
					return err
				}
			}

			rep, err := hygiene.Duplicates(db, hygiene.DuplicatesOptions{
				Strategy:    strategy,
				Fuzzy:       dupFuzzy,
				Threshold:   dupThreshold,
				ContentKeys: contentKeys,
			})
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.DuplicatesResult{Report: rep, Limit: dupLimit})
			return nil
		},
	}
}

func invalidCommand() *cli.Command {
	return &cli.Command{
		Name:  "invalid",
		Usage: "Scan the library for malformed field values (DOI/ISBN/URL/date)",
		Description: `$ sci zot doctor invalid
$ sci zot doctor invalid --field doi,date
$ sci zot doctor invalid --limit 0 --json > invalid.json

Fields: doi, isbn, url, date. Defaults to all.
All invalid findings are graded SevWarn (citation-affecting).`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "field",
				Aliases:     []string{"f"},
				Usage:       "comma-separated fields to validate (doi,isbn,url,date)",
				Destination: &invalidFields,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all)",
				Destination: &invalidLimit,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fields, err := parseInvalidFieldList(invalidFields)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Invalid(db, fields)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.InvalidResult{Report: rep, Limit: invalidLimit})
			return nil
		},
	}
}

func orphansCommand() *cli.Command {
	return &cli.Command{
		Name:  "orphans",
		Usage: "Find structural orphans (empty collections, standalone attachments/notes, unused tags, uncollected items, missing files)",
		Description: `$ sci zot doctor orphans
$ sci zot doctor orphans --kind uncollected-item
$ sci zot doctor orphans --kind missing-file --check-files
$ sci zot doctor orphans --limit 0 --json > orphans.json

Default kinds: empty-collection, standalone-attachment,
standalone-note, unused-tag.

Opt-in kinds (pass via --kind):
  uncollected-item  Noisy if you don't organize with collections.
  missing-file      Requires --check-files; stats every attachment.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "kind",
				Aliases:     []string{"k"},
				Usage:       "comma-separated orphan kinds to scan",
				Destination: &orphansKinds,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "check-files",
				Usage:       "stat each imported attachment to detect missing files (slow)",
				Destination: &orphansCheckFiles,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings per kind (0 = all)",
				Destination: &orphansLimit,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			kinds, err := parseOrphanKindList(orphansKinds)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			opts := hygiene.OrphansOptions{
				Kinds:      kinds,
				CheckFiles: orphansCheckFiles,
				DataDir:    cfg.DataDir,
			}
			rep, err := hygiene.Orphans(db, opts)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.OrphansResult{Report: rep, Limit: orphansLimit})
			return nil
		},
	}
}

func citekeysCommand() *cli.Command {
	return &cli.Command{
		Name:  "citekeys",
		Usage: "Validate stored cite-keys against the {author}{year}-{words}-{ZOTKEY} spec",
		Description: `$ sci zot doctor citekeys
$ sci zot doctor citekeys --limit 0 --json > citekeys.json

Categories and severities:
  invalid       SevError   structurally broken (whitespace, BibTeX-illegal chars)
  collision     SevError   two or more items share the same cite-key
  non-canonical SevWarn    BibTeX-legal but does not match our v2 spec
                           (BBT camelCase, hand-authored, drifted v1)

Items with no stored cite-key at all are counted as 'unstored' in the
summary but emit no finding.

This is a report. Synthesizing canonical keys and writing them back
through the Zotero Web API is destructive against a BBT-managed library,
and lives in the zot binary as ` + "`zot fix citekeys`" + `.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all)",
				Destination: &citekeysLimit,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Citekeys(db)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.CitekeysResult{Report: rep, Limit: citekeysLimit})
			return nil
		},
	}
}

func parseOrphanKindList(s string) ([]hygiene.OrphanKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := lo.Filter(strings.Split(s, ","), func(p string, _ int) bool {
		return strings.TrimSpace(p) != ""
	})
	return lo.MapErr(parts, func(p string, _ int) (hygiene.OrphanKind, error) {
		return hygiene.ParseOrphanKind(strings.TrimSpace(p))
	})
}

func parseInvalidFieldList(s string) ([]hygiene.InvalidField, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := lo.Filter(strings.Split(s, ","), func(p string, _ int) bool {
		return strings.TrimSpace(p) != ""
	})
	return lo.MapErr(parts, func(p string, _ int) (hygiene.InvalidField, error) {
		return hygiene.ParseInvalidField(strings.TrimSpace(p))
	})
}

func parseStrategy(s string) (hygiene.Strategy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "doi":
		return hygiene.StrategyDOI, nil
	case "title":
		return hygiene.StrategyTitle, nil
	case "both", "":
		return hygiene.StrategyBoth, nil
	case "content":
		return hygiene.StrategyContent, nil
	default:
		return "", fmt.Errorf("unknown --strategy %q (want doi, title, both, or content)", s)
	}
}

// parseMissingFieldList splits a comma-separated flag value into
// hygiene.MissingField values. Empty input means "all fields".
func parseMissingFieldList(s string) ([]hygiene.MissingField, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := lo.Filter(strings.Split(s, ","), func(p string, _ int) bool {
		return strings.TrimSpace(p) != ""
	})
	return lo.MapErr(parts, func(p string, _ int) (hygiene.MissingField, error) {
		return hygiene.ParseMissingField(strings.TrimSpace(p))
	})
}
