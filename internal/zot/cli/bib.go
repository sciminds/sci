package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

// Bib-command flag destinations.
var (
	bibFormat    string
	bibOut       string
	bibRecursive bool
)

// bibDocExts are the file extensions `zot bib` scans when given a
// directory: markdown (vault notes), Quarto, and R Markdown manuscripts.
// Matched case-insensitively via bibDocExt — R Markdown conventionally
// capitalizes its extension (.Rmd).
var bibDocExts = []string{".md", ".markdown", ".qmd", ".rmd"}

// bibDocExt reports whether path carries one of bibDocExts, ignoring case.
func bibDocExt(path string) bool {
	return slices.Contains(bibDocExts, strings.ToLower(filepath.Ext(path)))
}

func bibCommand() *cli.Command {
	return &cli.Command{
		Name:  "bib",
		Usage: "Build a bibliography from the citations in a document or folder",
		Description: "Scans markdown / Quarto text for citation references —\n" +
			"pandoc @citekeys, [[wikilinks]], DOIs, arXiv ids, URLs — resolves\n" +
			"each against your library, and emits a bibliography of exactly\n" +
			"the cited items. References that don't resolve to exactly one\n" +
			"item are always listed, never silently dropped.\n\n" +
			"$ sci zot bib paper.qmd --out refs.bib\n" +
			"$ sci zot bib notes/ --recursive --format csl-json --out refs.json\n" +
			"$ sci zot bib draft.md            # biblatex to stdout",
		ArgsUsage: "<file-or-dir>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "biblatex", Usage: "output format: biblatex (alias: bibtex), csl-json", Destination: &bibFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file (enables drift-detection keymap sidecar)", Destination: &bibOut, Local: true},
			&cli.BoolFlag{Name: "recursive", Aliases: []string{"r"}, Usage: "with a directory, descend into subdirectories", Destination: &bibRecursive, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one file or directory")
			}
			files, err := collectBibTargets(cmd.Args().First(), bibRecursive)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no %s files found under %s", strings.Join(bibDocExts, "/"), cmd.Args().First())
			}

			var refs []bib.Ref
			for _, f := range files {
				raw, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				refs = append(refs, bib.ScanText(string(raw))...)
			}
			// Cross-file dedup: ScanText already dedups within one file.
			refs = lo.UniqBy(refs, func(r bib.Ref) string {
				return string(r.Kind) + "\x00" + strings.ToLower(r.Value)
			})

			// bib opts into --library all: a manuscript's refs legitimately
			// cross libraries, and resolving against the merged ListAll pool
			// is exactly the one-pass resolution zen deleted its own copy of.
			// Cross-library duplicates surface through the existing honesty
			// gate — >1 distinct match is ambiguous, never a guess.
			_, db, err := openLocalDBAllowAll(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.ListAll(local.ListFilter{})
			if err != nil {
				return err
			}
			resolved, unresolved := bib.Resolve(refs, items)

			export, err := runLibraryExport(resolved, bibFormat, bibOut)
			if err != nil {
				return err
			}
			res := zot.BibResult{
				Export:     export,
				Files:      files,
				References: len(refs),
				Resolved:   len(resolved),
				Unresolved: unresolved,
			}
			warns := append(localReadWarnings(db, ""), bibQualityWarning(resolved, scopeFromCtx(ctx))...)
			outputScoped(ctx, cmd, cmdutil.WithWarnings(res, warns...))
			return nil
		},
	}
}

// collectBibTargets expands a path argument into the ordered list of
// document files to scan. A file argument is taken as-is (any extension —
// the user knows what they're pointing at); a directory collects
// markdown/Quarto files, sorted for determinism, descending only when
// recursive is set. Hidden directories (.obsidian, .git, …) are skipped.
func collectBibTargets(path string, recursive bool) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var files []string
	if recursive {
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != path && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if bibDocExt(p) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		files = lo.FilterMap(entries, func(e fs.DirEntry, _ int) (string, bool) {
			if e.IsDir() || !bibDocExt(e.Name()) {
				return "", false
			}
			return filepath.Join(path, e.Name()), true
		})
	}
	slices.Sort(files)
	return files, nil
}
