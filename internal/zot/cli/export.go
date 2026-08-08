package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/pkg/citekey"
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

	searchExport       bool   // --export on `zot search`: emit a bibliography
	searchExportFormat string // --format on `zot search`: biblatex (default) or csl-json
	searchExportOut    string
	searchNotes        bool // --notes on `zot search`: filter to items with docling notes
)

// libraryExportCommand implements `zot export` — a top-level command that
// writes every item (optionally filtered) to stdout or a file.
func libraryExportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "Export your whole library as BibLaTeX, CSL-JSON, or an NDJSON mirror",
		Description: "$ sci zot export --out refs.bib\n" +
			"$ sci zot export --format csl-json --out refs.json\n" +
			"$ sci zot export --collection COLLAAA1 --out brain.bib\n" +
			"$ sci zot export --library all --format ndjson --out zotero-items.ndjson\n\n" +
			"ndjson is the item-plane MIRROR, not a bibliography: one kind-tagged\n" +
			"JSON object per line (collections first, then items), every record\n" +
			"stamped with its library. With --out it also writes a .meta.json\n" +
			"sidecar last, so a consumer can tell a finished dump from a partial one.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "biblatex", Usage: "output format: biblatex (alias: bibtex), csl-json, ndjson", Destination: &libExportFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file (enables the keymap or completeness sidecar)", Destination: &libExportOut, Local: true},
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "filter by collection key", Destination: &libExportCollection, Local: true},
			&cli.StringFlag{Name: "tag", Usage: "filter by tag name", Destination: &libExportTag, Local: true},
			&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by item type (e.g. journalArticle)", Destination: &libExportType, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// ndjson is the one format that opts into --library all: its
			// consumer wants the whole corpus in one file, and both
			// ListAll and ListCollections are converted through libIn.
			// The citation formats stay single-library — merging two
			// libraries into one .bib would emit the deliberate
			// cross-library duplicates as separate entries.
			isDump := zot.ExportFormat(libExportFormat).Canon() == zot.ExportNDJSON
			open := openLocalDB
			if isDump {
				open = openLocalDBAllowAll
			}
			_, db, err := open(ctx)
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
			if isDump {
				result, err := runLibraryDump(ctx, db, items, libExportOut)
				if err != nil {
					return err
				}
				outputScoped(ctx, cmd, result)
				return nil
			}
			result, err := runLibraryExport(items, libExportFormat, libExportOut)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, result)
			return nil
		},
	}
}

// runLibraryDump serializes the item plane as NDJSON. With an --out path
// it writes the body, then the .meta.json sidecar last; without one it
// streams to stdout and says plainly that no sidecar was written, because
// a dump with no completeness signal is a different artifact.
func runLibraryDump(ctx context.Context, db local.Reader, items []local.Item, outPath string) (zot.LibraryDumpResult, error) {
	// ListAll leaves Tags/Collections/Attachments empty — only Read fills
	// them, and only one item at a time. Those three are exactly the
	// item-plane joins the mirror exists to carry, so hydrate in bulk.
	if err := db.HydrateAll(items); err != nil {
		return zot.LibraryDumpResult{}, fmt.Errorf("hydrate items: %w", err)
	}
	// Enrich resolves the BBT `Citation Key:` line in Extra and the
	// synthesized fallback. Without it the mirror carries only pinned
	// Zotero 7 keys, and the consumer's citekey column is mostly empty.
	for i := range items {
		citekey.Enrich(&items[i])
	}

	collections, err := db.ListCollections()
	if err != nil {
		return zot.LibraryDumpResult{}, fmt.Errorf("list collections: %w", err)
	}

	scope := "personal"
	if h := libraryHolderFromCtx(ctx); h != nil && h.Resolved != nil {
		scope = string(h.Resolved.Scope)
	}
	in := zot.DumpInput{
		Scope:         scope,
		SchemaVersion: db.SchemaVersion(),
		Items:         items,
		Collections:   collections,
	}

	res := zot.LibraryDumpResult{Scope: scope}
	if outPath == "" {
		var buf bytes.Buffer
		stats, err := zot.DumpNDJSON(&buf, in)
		if err != nil {
			return res, err
		}
		res.Body, res.Stats = buf.String(), stats
		return res, nil
	}

	f, err := os.Create(outPath) //nolint:gosec // path is the user's own --out
	if err != nil {
		return res, err
	}
	stats, dumpErr := zot.DumpNDJSON(f, in)
	closeErr := f.Close()
	if dumpErr != nil {
		return res, dumpErr
	}
	if closeErr != nil {
		return res, closeErr
	}
	res.OutPath, res.Stats = outPath, stats

	meta := zot.DumpMeta{Scope: scope, SchemaVersion: db.SchemaVersion(), Stats: stats}
	if t, ok := db.LastSync(); ok {
		meta.LastSync = t.UTC().Format(time.RFC3339)
	}
	if n, ok := db.PendingWAL(); ok {
		meta.PendingWAL = n
	}
	metaPath, err := zot.WriteDumpMeta(outPath, meta)
	if err != nil {
		return res, err
	}
	res.MetaPath = metaPath
	return res, nil
}

// runLibraryExport is the shared pipeline used by `zot export` and
// `zot search --export`. It loads the prior keymap (if -o was given),
// invokes zot.ExportLibrary, writes the body to the chosen sink, and
// persists the updated keymap alongside the .bib file for next-run drift
// detection.
func runLibraryExport(items []local.Item, format, outPath string) (zot.LibraryExportResult, error) {
	fmtEnum := zot.ExportFormat(format).Canon()
	switch fmtEnum {
	case zot.ExportBibLaTeX, zot.ExportCSLJSON, "":
	case zot.ExportNDJSON:
		// Reachable from `search --export`, which shares this pipeline but
		// is a bibliography surface. A dump of arbitrary search hits is not
		// a coherent item plane — name the command that is.
		return zot.LibraryExportResult{}, fmt.Errorf("ndjson is a whole-library mirror, not a bibliography format; use: sci zot export --format ndjson")
	default:
		return zot.LibraryExportResult{}, fmt.Errorf("unknown format %q (want biblatex, csl-json, or ndjson)", format)
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
