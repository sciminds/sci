package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/fix"
	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/sciminds/cli/internal/zot/local"
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
	citekeysFix   bool
	citekeysApply bool
	citekeysKind  []string
	citekeysItem  []string
	citekeysYes   bool
)

func missingCommand() *cli.Command {
	return &cli.Command{
		Name:  "missing",
		Usage: "Scan the library for items missing common fields",
		Description: `$ zot doctor missing
$ zot doctor missing --field title,creators
$ zot doctor missing --field doi,abstract
$ zot doctor missing --limit 0 --json > coverage.json

Fields: title, creators, date, doi, abstract, url, pdf, tags. Defaults to all.
Severity: title=error, creators/date=warn, others=info.`,
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
		Action: func(_ context.Context, cmd *cli.Command) error {
			fields, err := parseMissingFieldList(missingFields)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Missing(db, fields)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.MissingResult{Report: rep, Limit: missingLimit})
			return nil
		},
	}
}

func duplicatesCommand() *cli.Command {
	return &cli.Command{
		Name:  "duplicates",
		Usage: "Find potential duplicate items (by DOI and/or title)",
		Description: `$ zot doctor duplicates                  # fast: DOI + exact-normalized title
$ zot doctor duplicates --fuzzy          # adds slow fuzzy title pass (~30s on 5k items)
$ zot doctor duplicates --strategy doi
$ zot doctor duplicates --fuzzy --threshold 0.9
$ zot doctor duplicates --limit 0 --json > dupes.json

Strategies: doi (strongest), title, both (default).

Fast mode catches shared DOIs and titles that are identical after
normalization (case/whitespace/punctuation). Use --fuzzy to additionally
pair near-identical titles (typos, punctuation drift) using Levenshtein
similarity — accurate but O(n²) on the title-only singletons.

DOI matches subsume title matches when both fire on the same items.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "strategy",
				Aliases:     []string{"s"},
				Value:       "both",
				Usage:       "match strategy: doi, title, both",
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
		Action: func(_ context.Context, cmd *cli.Command) error {
			strategy, err := parseStrategy(dupStrategy)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			if dupThreshold < 0 || dupThreshold > 1 {
				return cmdutil.UsageErrorf(cmd, "--threshold must be in [0, 1]")
			}
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Duplicates(db, hygiene.DuplicatesOptions{
				Strategy:  strategy,
				Fuzzy:     dupFuzzy,
				Threshold: dupThreshold,
			})
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.DuplicatesResult{Report: rep, Limit: dupLimit})
			return nil
		},
	}
}

func invalidCommand() *cli.Command {
	return &cli.Command{
		Name:  "invalid",
		Usage: "Scan the library for malformed field values (DOI/ISBN/URL/date)",
		Description: `$ zot doctor invalid
$ zot doctor invalid --field doi,date
$ zot doctor invalid --limit 0 --json > invalid.json

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
		Action: func(_ context.Context, cmd *cli.Command) error {
			fields, err := parseInvalidFieldList(invalidFields)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Invalid(db, fields)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.InvalidResult{Report: rep, Limit: invalidLimit})
			return nil
		},
	}
}

func orphansCommand() *cli.Command {
	return &cli.Command{
		Name:  "orphans",
		Usage: "Find structural orphans (empty collections, standalone attachments/notes, unused tags, uncollected items, missing files)",
		Description: `$ zot doctor orphans
$ zot doctor orphans --kind uncollected-item
$ zot doctor orphans --kind missing-file --check-files
$ zot doctor orphans --limit 0 --json > orphans.json

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
		Action: func(_ context.Context, cmd *cli.Command) error {
			kinds, err := parseOrphanKindList(orphansKinds)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%s", err.Error())
			}
			cfg, db, err := openLocalDB()
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
			cmdutil.Output(cmd, zot.OrphansResult{Report: rep, Limit: orphansLimit})
			return nil
		},
	}
}

func citekeysCommand() *cli.Command {
	return &cli.Command{
		Name:  "citekeys",
		Usage: "Validate stored cite-keys against the {author}{year}-{words}-{ZOTKEY} spec",
		Description: `$ zot doctor citekeys                     # read-only check
$ zot doctor citekeys --limit 0 --json > citekeys.json

$ zot doctor citekeys --fix               # dry-run: preview what would change
$ zot doctor citekeys --fix --apply       # actually write through Zotero Web API
$ zot doctor citekeys --fix --apply --kind invalid,collision
$ zot doctor citekeys --fix --apply --item ABCD1234
$ zot doctor citekeys --fix --apply --yes

Categories and severities:
  invalid       SevError   structurally broken (whitespace, BibTeX-illegal chars)
  collision     SevError   two or more items share the same cite-key
  non-canonical SevWarn    BibTeX-legal but does not match our v2 spec
                           (BBT camelCase, hand-authored, drifted v1)

Items with no stored cite-key at all are counted as 'unstored' in the
summary but emit no finding — ` + "`--fix`" + ` will synthesize canonical keys for
them and write them back through the Zotero Web API.

Fix safety: --fix is dry-run by default. --apply is required to
actually patch items. --kind defaults to every bucket (invalid +
collision + non-canonical + unstored); narrow it on a BBT-managed
library to avoid rewriting every key in one pass. --item restricts
to a specific Zotero key, useful for smoke-testing a single write.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings (or targets) to print (0 = all)",
				Destination: &citekeysLimit,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "fix",
				Usage:       "switch from read-only check to repair mode (dry-run unless --apply)",
				Destination: &citekeysFix,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "apply",
				Usage:       "with --fix, actually write cite-key patches through the Zotero Web API",
				Destination: &citekeysApply,
				Local:       true,
			},
			&cli.StringSliceFlag{
				Name:        "kind",
				Aliases:     []string{"k"},
				Usage:       "with --fix, limit to buckets (invalid,collision,non-canonical,unstored)",
				Destination: &citekeysKind,
				Local:       true,
			},
			&cli.StringSliceFlag{
				Name:        "item",
				Usage:       "with --fix, only touch these Zotero item keys (repeatable)",
				Destination: &citekeysItem,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "yes",
				Aliases:     []string{"y"},
				Usage:       "with --fix --apply, skip the confirmation prompt",
				Destination: &citekeysYes,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if citekeysFix {
				return runCitekeysFix(ctx, cmd)
			}
			// Read-only path: unchanged from the earlier slice.
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.Citekeys(db)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.CitekeysResult{Report: rep, Limit: citekeysLimit})
			return nil
		},
	}
}

// runCitekeysFix is the --fix path: plan targets, optionally apply, and
// render a FixResult. Kept in its own function so the read-only action
// stays small and obvious.
func runCitekeysFix(ctx context.Context, cmd *cli.Command) error {
	// Resolve kind bitmask from --kind flags. Empty = all buckets.
	kinds, err := parseCitekeyFixKinds(citekeysKind)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%s", err.Error())
	}

	_, db, err := openLocalDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// ListAll with no filter gives us every content item already
	// hydrated with Fields + Creators — the planner needs both.
	items, err := db.ListAll(local.ListFilter{})
	if err != nil {
		return fmt.Errorf("list items: %w", err)
	}
	targets := fix.PlanCitekeys(items, fix.CitekeyOptions{
		Kinds:    kinds,
		ItemKeys: citekeysItem,
	})

	if !citekeysApply {
		// Dry-run: render the plan and exit. No API client needed.
		cmdutil.Output(cmd, fix.CitekeyFixResult{
			Result: fix.DryRunCitekeys(targets),
			Limit:  citekeysLimit,
		})
		return nil
	}

	if len(targets) == 0 {
		// Nothing to apply — still render so JSON callers see the empty
		// totals and human callers get the "nothing to do" line.
		cmdutil.Output(cmd, fix.CitekeyFixResult{
			Result: fix.DryRunCitekeys(targets),
			Limit:  citekeysLimit,
		})
		return nil
	}

	// Destructive confirm, matching the pattern every other write
	// command uses. --yes bypasses for non-interactive runs.
	prompt := fmt.Sprintf("patch %d item(s) citationKey field via Zotero Web API?", len(targets))
	if done, err := cmdutil.ConfirmOrSkip(citekeysYes, prompt); done || err != nil {
		return err
	}

	apiClient, err := requireAPIClient()
	if err != nil {
		return err
	}

	var res *fix.CitekeyResult
	err = ui.RunWithProgress("Fixing cite-keys", func(t *ui.ProgressTracker) error {
		t.SetTotal(len(targets))
		var applyErr error
		res, applyErr = fix.ApplyCitekeys(ctx, apiClient, targets, fix.ApplyOptions{
			OnProgress: func(done, total int) {
				counter := "patched"
				t.Advance(counter, "")
			},
		})
		return applyErr
	})
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, fix.CitekeyFixResult{Result: res, Limit: citekeysLimit})
	return nil
}

// parseCitekeyFixKinds turns --kind values into a fix.CitekeyKind mask.
// Empty input → fix.CitekeyAll. Accepts repeatable + comma-separated
// forms uniformly so `--kind invalid --kind collision` and
// `--kind invalid,collision` behave the same.
func parseCitekeyFixKinds(values []string) (fix.CitekeyKind, error) {
	if len(values) == 0 {
		return fix.CitekeyAll, nil
	}
	var mask fix.CitekeyKind
	for _, raw := range values {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			bit, ok := fix.ParseCitekeyKind(p)
			if !ok {
				return 0, fmt.Errorf("unknown --kind %q (want invalid, collision, non-canonical, unstored)", p)
			}
			mask |= bit
		}
	}
	if mask == 0 {
		return fix.CitekeyAll, nil
	}
	return mask, nil
}

func parseOrphanKindList(s string) ([]hygiene.OrphanKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]hygiene.OrphanKind, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k, err := hygiene.ParseOrphanKind(p)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func parseInvalidFieldList(s string) ([]hygiene.InvalidField, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]hygiene.InvalidField, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := hygiene.ParseInvalidField(p)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func parseStrategy(s string) (hygiene.Strategy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "doi":
		return hygiene.StrategyDOI, nil
	case "title":
		return hygiene.StrategyTitle, nil
	case "both", "":
		return hygiene.StrategyBoth, nil
	default:
		return "", fmt.Errorf("unknown --strategy %q (want doi, title, or both)", s)
	}
}

// parseMissingFieldList splits a comma-separated flag value into
// hygiene.MissingField values. Empty input means "all fields".
func parseMissingFieldList(s string) ([]hygiene.MissingField, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]hygiene.MissingField, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := hygiene.ParseMissingField(p)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
