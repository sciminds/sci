package cli

// `zot import <path>` is the desktop-connector drag-drop equivalent: it POSTs
// a local PDF to Zotero desktop's 127.0.0.1:23119/connector/ endpoint, and
// desktop handles ingestion, sync-to-cloud, AND metadata recognition the same
// way it would for a drag-and-drop. Requires Zotero desktop to be running.
//
// This is intentionally a separate command from `zot item add --file` (Web
// API, headless, no recognition). Users pick based on whether desktop is
// available: import when you want real metadata, item add --file when you
// don't care or don't have desktop up.

import (
	"context"
	"fmt"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/connector"
	"github.com/urfave/cli/v3"
)

var (
	importTimeout time.Duration
	importNoWait  bool
)

func importCommand() *cli.Command {
	return &cli.Command{
		Name:      "import",
		Usage:     "Import a local PDF via Zotero desktop (drag-drop equivalent, with metadata recognition)",
		ArgsUsage: "<path>",
		Description: "$ zot import ~/papers/Smith2022.pdf\n" +
			"$ zot import ~/papers/Smith2022.pdf --timeout 90s\n" +
			"$ zot import ~/papers/Smith2022.pdf --no-wait\n" +
			"\n" +
			"Posts the PDF to Zotero desktop's local connector server\n" +
			"(127.0.0.1:23119) and waits for desktop to finish the same metadata\n" +
			"recognition it runs on drag-drop — CrossRef/arXiv lookup via the\n" +
			"extracted DOI/arXiv ID, creating a parent bibliographic item and\n" +
			"re-parenting the attachment under it.\n" +
			"\n" +
			"Requires Zotero desktop to be running. Uses desktop's currently\n" +
			"selected library — not the --library flag — which is why this command\n" +
			"is exempt from --library. For a headless Web-API upload (no recognition,\n" +
			"standalone attachment), use `zot item add --file` instead.",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:        "timeout",
				Value:       connector.DefaultTimeout,
				Usage:       "how long to wait for recognition before returning a partial result",
				Destination: &importTimeout,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "no-wait",
				Usage:       "return immediately after upload; skip polling for recognition",
				Destination: &importNoWait,
				Local:       true,
			},
		},
		Action: runImport,
	}
}

func runImport(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cmdutil.UsageErrorf(cmd, "expected exactly one <path> argument")
	}
	path := cmd.Args().First()

	c := connector.NewClient()
	res, err := connector.Import(ctx, c, path, connector.Options{
		Timeout: importTimeout,
		NoWait:  importNoWait,
	})
	if err != nil {
		return fmt.Errorf("zot import: %w", err)
	}
	cmdutil.Output(cmd, zot.ImportResult{
		Path:       res.Path,
		Recognized: res.Recognized,
		Title:      res.Title,
		ItemType:   res.ItemType,
		Message:    res.Message,
	})
	return nil
}
