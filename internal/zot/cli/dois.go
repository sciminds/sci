package cli

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/fix"
	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// dois flag destinations.
var (
	doisLimit int
	doisFix   bool
	doisApply bool
	doisYes   bool
)

func doisCommand() *cli.Command {
	return &cli.Command{
		Name:  "dois",
		Usage: "Scan for publisher subobject DOIs (Frontiers/abstract, PLOS .tNNN, PNAS supplements)",
		Description: `$ sci zot --library personal doctor dois                       # read-only scan
$ sci zot --library personal doctor dois --json > dois.json

$ sci zot --library personal doctor dois --fix                 # dry-run repair preview
$ sci zot --library personal doctor dois --fix --apply         # write through Zotero Web API
$ sci zot --library personal doctor dois --fix --apply --yes   # skip confirmation

A 'subobject DOI' is a DOI that points at a part of a paper (a table,
figure, supplement, or article-section anchor) rather than the parent
work. These DOIs 404 on OpenAlex and other metadata APIs, so any
PDF/landing-page lookup for the item silently fails.

Patterns recognized (anchored to the publisher prefix):
  Frontiers          10.3389/.../abstract  10.3389/.../full
  PLOS subobjects    10.1371/....tNNN  ....gNNN  ....sNNN
  PNAS supplements   10.1073/.../-/DCSupplemental[/...]

--fix is dry-run by default. --apply is required to actually patch the
DOI field via the Zotero Web API; the new DOI is the parent-paper form
the suffix-stripper produces. Confirmation required unless --yes is
passed.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings (or targets) to print (0 = all)",
				Destination: &doisLimit,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "fix",
				Usage:       "switch from read-only check to repair mode (dry-run unless --apply)",
				Destination: &doisFix,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "apply",
				Usage:       "with --fix, actually write DOI patches through the Zotero Web API",
				Destination: &doisApply,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "yes",
				Aliases:     []string{"y"},
				Usage:       "with --fix --apply, skip the confirmation prompt",
				Destination: &doisYes,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if !doisFix && doisApply {
				return cmdutil.UsageErrorf(cmd, "--apply requires --fix")
			}
			if doisFix {
				return runDOIsFix(ctx, cmd)
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			rep, err := hygiene.SubobjectDOIs(db)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.SubobjectDOIsResult{Report: rep, Limit: doisLimit})
			return nil
		},
	}
}

// runDOIsFix is the --fix path: plan targets, optionally apply, render.
func runDOIsFix(ctx context.Context, cmd *cli.Command) error {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	items, err := db.ListAll(local.ListFilter{})
	if err != nil {
		return fmt.Errorf("list items: %w", err)
	}
	targets := fix.PlanDOIs(items)

	if !doisApply || len(targets) == 0 {
		outputScoped(ctx, cmd, fix.DOIFixResult{
			Result: fix.DryRunDOIs(targets),
			Limit:  doisLimit,
		})
		return nil
	}

	prompt := fmt.Sprintf("patch %d item(s) DOI field via Zotero Web API?", len(targets))
	if done, err := cmdutil.ConfirmOrSkip(doisYes, prompt); done || err != nil {
		return err
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	var res *fix.DOIResult
	err = uikit.RunWithProgress("Fixing subobject DOIs", func(t *uikit.ProgressTracker) error {
		t.SetTotal(len(targets))
		var applyErr error
		res, applyErr = fix.ApplyDOIs(ctx, apiClient, targets, fix.ApplyOptions{
			OnProgress: func(_, _ int) {
				t.Advance("patched", "")
			},
		})
		return applyErr
	})
	if err != nil {
		return err
	}
	outputScoped(ctx, cmd, fix.DOIFixResult{Result: res, Limit: doisLimit})
	return nil
}
