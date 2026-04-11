package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// keymapFilename is the sidecar written next to a .bib export, used for
// drift detection on the next run. See internal/zot/exportlib.go for the
// design rationale.
const keymapFilename = ".zotero-citekeymap.json"

// Library export flag destinations. Kept separate from the single-item
// exporter's flags (exportFormat/exportOut in read.go) so the two commands
// don't trample each other when both parse state from a shared test process.
var (
	libExportFormat     string
	libExportOut        string
	libExportCollection string
	libExportTag        string
	libExportType       string

	searchExport    bool // --export on `zot search`: emit bibtex
	searchExportOut string
)

// libraryExportCommand implements `zot export` — a top-level command that
// writes every item (optionally filtered) to stdout or a file.
func libraryExportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "Export your whole library as BibTeX or CSL-JSON",
		Description: "$ zot export --out refs.bib\n" +
			"$ zot export --format csl-json --out refs.json\n" +
			"$ zot export --collection COLLAAA1 --out brain.bib",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "bibtex", Usage: "output format: bibtex, csl-json", Destination: &libExportFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file (enables drift-detection keymap sidecar)", Destination: &libExportOut, Local: true},
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "filter by collection key", Destination: &libExportCollection, Local: true},
			&cli.StringFlag{Name: "tag", Usage: "filter by tag name", Destination: &libExportTag, Local: true},
			&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by item type (e.g. journalArticle)", Destination: &libExportType, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.ListAll(local.ListFilter{
				ItemType:      libExportType,
				CollectionKey: libExportCollection,
				Tag:           libExportTag,
			})
			if err != nil {
				return err
			}
			result, err := runLibraryExport(items, libExportFormat, libExportOut)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

// runLibraryExport is the shared pipeline used by `zot export` and
// `zot search --export`. It loads the prior keymap (if -o was given),
// invokes zot.ExportLibrary, writes the body to the chosen sink, and
// persists the updated keymap alongside the .bib file for next-run drift
// detection.
func runLibraryExport(items []local.Item, format, outPath string) (zot.LibraryExportResult, error) {
	fmtEnum := zot.ExportFormat(format)
	switch fmtEnum {
	case zot.ExportBibTeX, zot.ExportCSLJSON, "":
	default:
		return zot.LibraryExportResult{}, fmt.Errorf("unknown format %q (want bibtex or csl-json)", format)
	}

	var prev zot.Keymap
	keymapPath := ""
	if outPath != "" {
		keymapPath = filepath.Join(filepath.Dir(outPath), keymapFilename)
		loaded, err := zot.LoadKeymap(keymapPath)
		if err != nil {
			return zot.LibraryExportResult{}, fmt.Errorf("load keymap: %w", err)
		}
		prev = loaded
	}

	body, stats, err := zot.ExportLibrary(items, fmtEnum, prev)
	if err != nil {
		return zot.LibraryExportResult{}, err
	}

	res := zot.LibraryExportResult{
		Format: string(fmtEnum),
		Stats:  stats,
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			return res, err
		}
		res.OutPath = outPath
		// Only write the keymap sidecar when we have synthesized entries
		// to track. If a subsequent run has zero synthesized items, we
		// deliberately do NOT clobber an existing sidecar — that file
		// may still be load-bearing for other exports in the same dir
		// (e.g. a full-library .bib next to a filtered search-export).
		if len(stats.Keymap) > 0 {
			if err := zot.SaveKeymap(keymapPath, stats.Keymap); err != nil {
				return res, fmt.Errorf("save keymap: %w", err)
			}
			res.KeymapPath = keymapPath
		}
	} else {
		res.Body = body
	}
	return res, nil
}
