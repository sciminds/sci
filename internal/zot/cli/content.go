package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/content"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// content-command flag destinations (package-scoped).
var (
	contentBuildDryRun  bool
	contentBuildRebuild bool
	contentBuildQuiet   bool
)

func contentCommand() *cli.Command {
	return &cli.Command{
		Name:  "content",
		Usage: "Paper text end to end — extract it, list it, read it, index it",
		Description: "An extraction is the PAPER, not a note. This namespace owns the\n" +
			"text of your papers from the docling run that produces it through\n" +
			"the searchable index built over it. `zot notes` means the notes\n" +
			"YOU wrote.\n\n" +
			"$ sci zot content extract AAAA1111 --apply  # run docling, post the text\n" +
			"$ sci zot content refresh AAAA1111          # re-extract in place\n" +
			"$ sci zot content list                      # items that have an extraction\n" +
			"$ sci zot content read AAAA1111             # the text of one paper\n" +
			"$ sci zot content drop AAAA1111             # trash its extraction\n" +
			"$ sci zot content build                     # create or refresh the index\n" +
			"$ sci zot content stats                     # coverage, size, staleness",
		Commands: []*cli.Command{
			contentExtractCommand(),
			contentRefreshCommand(),
			contentListCommand(),
			contentReadCommand(),
			contentDropCommand(),
			contentBuildCommand(),
			contentStatsCommand(),
		},
	}
}

func contentBuildCommand() *cli.Command {
	return &cli.Command{
		Name:    "build",
		Aliases: []string{"index", "reindex"},
		Usage:   "Create or refresh the content index",
		Description: "Incremental by default: only items whose extraction or attachment\n" +
			"changed are re-read. A first build over a few thousand papers takes\n" +
			"about a minute; refreshes are near-instant.\n\n" +
			"$ sci zot content build\n" +
			"$ sci zot content build --dry-run   # what would change\n" +
			"$ sci zot content build --rebuild   # discard and index from scratch",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "report what would be indexed without writing", Destination: &contentBuildDryRun, Local: true},
			&cli.BoolFlag{Name: "rebuild", Usage: "discard the existing index and build from scratch", Destination: &contentBuildRebuild, Local: true},
			&cli.BoolFlag{Name: "quiet", Usage: "suppress the progress ticker", Destination: &contentBuildQuiet, Local: true},
		},
		Action: contentBuildAction,
	}
}

func contentBuildAction(ctx context.Context, cmd *cli.Command) error {
	if contentBuildDryRun && contentBuildRebuild {
		return cmdutil.Coded(cmdutil.CodeConflict, "--dry-run and --rebuild are mutually exclusive").
			WithTry("drop --rebuild to preview, or drop --dry-run to actually rebuild")
	}
	cfg, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	path, err := content.DefaultPath(db.LibraryID())
	if err != nil {
		return err
	}
	if contentBuildRebuild {
		// Remove the sidecars too, or SQLite reopens onto a stale WAL.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("discard existing index: %w", err)
			}
		}
	}

	ix, err := content.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()

	plan, err := content.PlanSync(ix, db)
	if err != nil {
		return err
	}
	res := zot.ContentBuildResult{
		Path:    path,
		Planned: plan.Total(),
		DryRun:  contentBuildDryRun,
	}
	if contentBuildDryRun {
		res.Added, res.Updated, res.Deleted = len(plan.Add), len(plan.Update), len(plan.Delete)
		outputScoped(ctx, cmd, res)
		return nil
	}

	start := time.Now()
	built, err := content.Build(ix, plan, content.ZoteroLoader(db, cfg.DataDir), content.Options{
		Progress: contentProgress(cmd, plan.Total()),
	})
	if err != nil {
		return err
	}
	// Merging b-tree segments after a bulk load is what keeps query
	// latency flat as the index grows.
	if built.Added+built.Updated+built.Deleted > 0 {
		if err := ix.Vacuum(); err != nil {
			return err
		}
	}
	// Stamp what this index now reflects. Without it the index carries no
	// fingerprint, content.Stale reads that as "never built", and the
	// staleness warning on `search --content` can never fire.
	if err := content.RecordBuilt(ix, db); err != nil {
		return err
	}

	res.Added, res.Updated, res.Deleted = built.Added, built.Updated, built.Deleted
	res.Skipped, res.Failed = built.Skipped, built.Failed
	res.BySource = sourceCounts(built.BySource)
	res.Duration = time.Since(start)
	outputScoped(ctx, cmd, res)
	return nil
}

// contentProgress returns a ticker for long builds, or nil when output
// must stay clean (--json, --quiet, or a small plan).
func contentProgress(cmd *cli.Command, total int) func(done, total int) {
	if contentBuildQuiet || cmdutil.IsJSON(cmd) || total < content.DefaultBatchSize {
		return nil
	}
	return func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %s indexing %d/%d", uikit.SymArrow, done, total)
		if done >= total {
			fmt.Fprintln(os.Stderr)
		}
	}
}

func contentStatsCommand() *cli.Command {
	return &cli.Command{
		Name:        "stats",
		Usage:       "Show content-index coverage, size, and staleness",
		Description: "$ sci zot content stats",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			ix, err := openContentIndex(db)
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()

			st, err := ix.Stats()
			if err != nil {
				return err
			}
			plan, err := content.PlanSync(ix, db)
			if err != nil {
				return err
			}
			cands, err := content.Candidates(db)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.ContentStatsResult{
				Path:       ix.Path(),
				Indexed:    st.Total,
				BySource:   sourceCounts(st.BySource),
				Bytes:      st.Bytes,
				Pending:    plan.Total(),
				Candidates: len(cands),
			})
			return nil
		},
	}
}

func contentReadCommand() *cli.Command {
	return &cli.Command{
		Name:  "read",
		Usage: "Print the indexed text of one item",
		Description: "The full text sci has for a paper, from whichever source supplied\n" +
			"it. Useful for piping a paper into a model.\n\n" +
			"$ sci zot content read AAAA1111\n" +
			"$ sci zot content read AAAA1111 --json",
		ArgsUsage: "<item-key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one item key")
			}
			itemKey := cmd.Args().First()

			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			ix, err := openContentIndex(db)
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()

			body, source, ok, err := ix.Body(itemKey)
			if err != nil {
				return err
			}
			if !ok {
				return cmdutil.Coded(cmdutil.CodeNotFound,
					"no indexed content for %s", itemKey).
					WithTry("the item may have no PDF text, or the index may be stale — " +
						"check `sci zot content stats`")
			}
			outputScoped(ctx, cmd, zot.ContentReadResult{
				ItemKey: itemKey,
				Source:  string(source),
				Chars:   len(body),
				Body:    body,
			})
			return nil
		},
	}
}

// openContentIndex opens the index for the DB's library. It does not
// build one — commands that read the index tell the user to build it
// rather than silently spending a minute on their behalf.
func openContentIndex(db local.Reader) (*content.Index, error) {
	path, err := content.DefaultPath(db.LibraryID())
	if err != nil {
		return nil, err
	}
	return content.Open(path)
}

// sourceCounts converts the typed by-source map into the plain string
// keys the JSON contract exposes.
func sourceCounts(in map[content.Source]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	return lo.MapKeys(in, func(_ int, s content.Source) string { return string(s) })
}
