package cli

import (
	"context"

	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/hygiene"
	"github.com/urfave/cli/v3"
)

// dois flag destinations.
var (
	doisLimit int
)

func doisCommand() *cli.Command {
	return &cli.Command{
		Name:  "dois",
		Usage: "Scan for publisher subobject DOIs (article-section, table, figure, and supplement children)",
		Description: `$ sci zot --library personal doctor dois
$ sci zot --library personal doctor dois --json > dois.json

A 'subobject DOI' is a DOI that points at a part of a paper (a table,
figure, supplement, or article-section anchor) rather than the parent
work. These DOIs 404 on OpenAlex and other metadata APIs, so any
PDF/landing-page lookup for the item silently fails.

Patterns recognized (anchored to the publisher prefix, case-insensitive):
  article sections   10.3389|10.1111|10.1002/.../abstract  .../full
  PLOS subobjects    10.1371/....tNNN  ....gNNN  ....sNNN
  supplements        10.1073|10.1093/.../-/DCSupplemental[/...]  .../-/DCn
  eLife assets       10.7554/eLife.NNNNN.NNN
  PeerJ subobjects   10.7717/peerj.NNN/supp-N  /fig-N  /table-N

This is a report. Rewriting the stored DOI to its parent-paper form is a
write against the Zotero Web API and lives in the zot binary, as
` + "`zot fix dois`" + `.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all)",
				Destination: &doisLimit,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
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
