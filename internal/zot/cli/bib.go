package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/bib"
	"github.com/sciminds/cli/internal/zot/doiorg"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
	"github.com/urfave/cli/v3"
)

// Bib-command flag destinations.
var (
	bibFormat    string
	bibOut       string
	bibRecursive bool
	bibVerify    bool
)

// workResolver is the one openalex.Client method the verification lookup
// needs — narrowed so the adapter is testable without HTTP.
type workResolver interface {
	ResolveWork(ctx context.Context, identifier string) (*openalex.Work, error)
}

// openAlexLookup adapts the OpenAlex client to [bib.Lookup]: it turns a
// citation reference into an upstream identifier, and a non-2xx response into
// the right kind of failure. A 404 becomes [bib.ErrNotFound] (evidence the
// citation is invented); everything else stays an error (evidence only that
// the network misbehaved).
type openAlexLookup struct{ c workResolver }

func (l openAlexLookup) ResolveRef(ctx context.Context, ref bib.Ref) (*bib.Match, error) {
	id := ref.Value
	if ref.Kind == bib.KindArxiv {
		// A bare "1706.03762" reads as an arXiv id to NormalizeID only by
		// pattern; saying so explicitly keeps the lookup deterministic.
		id = "arxiv:" + id
	}
	w, err := l.c.ResolveWork(ctx, id)
	if err != nil {
		if serr, ok := errors.AsType[*openalex.StatusError](err); ok && serr.Code == http.StatusNotFound {
			return nil, bib.ErrNotFound
		}
		return nil, err
	}
	return matchFromWork(w), nil
}

// doiResolver is the one doiorg.Client method the registry lookup needs.
type doiResolver interface {
	Resolve(ctx context.Context, doi string) (*doiorg.Record, error)
}

// registryLookup adapts the doi.org registry to [bib.Lookup]. It is the
// authoritative leg of the verification chain: doi.org fronts every
// registrar, so its 404 is the only sound basis for calling a citation
// invented. It carries less metadata than a citation index and — notably —
// knows nothing about retractions, so it never sets that flag.
type registryLookup struct{ c doiResolver }

func (l registryLookup) ResolveRef(ctx context.Context, ref bib.Ref) (*bib.Match, error) {
	target := ref.Value
	if ref.Kind == bib.KindArxiv {
		// arXiv registers a DataCite DOI per preprint, which puts preprints
		// on the same registry path as everything else.
		target = doiorg.ArxivDOI(ref.Value)
	}
	rec, err := l.c.Resolve(ctx, target)
	if err != nil {
		if errors.Is(err, doiorg.ErrNotFound) {
			return nil, bib.ErrNotFound
		}
		return nil, err
	}
	return &bib.Match{
		DOI:   cmp.Or(rec.DOI, target),
		Title: rec.Title,
		Year:  rec.Year,
		Venue: rec.Venue,
	}, nil
}

// matchFromWork projects an OpenAlex work onto the compact [bib.Match],
// stripping the URL wrappers OpenAlex puts on ids so both the DOI and the
// short id can be pasted straight into `item add --openalex`.
func matchFromWork(w *openalex.Work) *bib.Match {
	m := &bib.Match{
		OpenAlexID: strings.TrimPrefix(strings.TrimPrefix(w.ID, "https://"), "openalex.org/"),
		Retracted:  w.IsRetracted,
	}
	if w.DOI != nil {
		m.DOI = strings.TrimPrefix(strings.TrimPrefix(*w.DOI, "https://"), "doi.org/")
	}
	if w.Title != nil {
		m.Title = *w.Title
	} else if w.DisplayName != nil {
		m.Title = *w.DisplayName
	}
	if w.PublicationYear != nil {
		m.Year = *w.PublicationYear
	}
	if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
		m.Venue = w.PrimaryLocation.Source.DisplayName
	}
	return m
}

// bibDocExts are the file extensions `zot bib` scans when given a
// directory: markdown (vault notes) and Quarto manuscripts.
var bibDocExts = []string{".md", ".markdown", ".qmd"}

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
			"$ sci zot bib draft.md            # bibtex to stdout\n" +
			"$ sci zot bib draft.md --verify   # also: which unresolved refs are real?",
		ArgsUsage: "<file-or-dir>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "bibtex", Usage: "output format: bibtex, csl-json", Destination: &bibFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file (enables drift-detection keymap sidecar)", Destination: &bibOut, Local: true},
			&cli.BoolFlag{Name: "recursive", Aliases: []string{"r"}, Usage: "with a directory, descend into subdirectories", Destination: &bibRecursive, Local: true},
			&cli.BoolFlag{Name: "verify", Usage: "check unresolved DOIs / arXiv ids against OpenAlex: real-but-missing vs. resolves-nowhere (needs network)", Destination: &bibVerify, Local: true},
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

			_, db, err := openLocalDB(ctx)
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
			if bibVerify && len(unresolved) > 0 {
				res.Verified, err = verifyUnresolved(ctx, unresolved, scopeFromCtx(ctx))
				if err != nil {
					return err
				}
			}
			warns := append(staleLocalWarning(db, ""), bibQualityWarning(resolved, scopeFromCtx(ctx))...)
			outputScoped(ctx, cmd, cmdutil.WithWarnings(res, warns...))
			return nil
		},
	}
}

// verifyUnresolved classifies the references that didn't resolve locally
// against OpenAlex and attaches a runnable fix to each verdict that has one.
// Requires the network — the whole point is asking an index we don't hold.
func verifyUnresolved(ctx context.Context, unresolved []bib.Unresolved, scope string) ([]bib.Verified, error) {
	if !netutil.Online() {
		return nil, cmdutil.Coded(cmdutil.CodeOffline,
			"--verify needs network access to reach OpenAlex").
			WithTry("drop --verify to get the unresolved list without upstream classification")
	}
	client, err := openalexClient()
	if err != nil {
		return nil, err
	}
	// Citation index first (rich metadata, retraction flags), DOI registry
	// second (authoritative existence). Only when both miss is a reference
	// reported as resolving nowhere.
	lookup := bib.ChainLookup{openAlexLookup{client}, registryLookup{doiorg.New()}}
	verified := bib.Verify(ctx, unresolved, lookup)
	for i := range verified {
		verified[i].Fix = bib.FixCommand(verified[i], scope)
	}
	return verified, nil
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
			if slices.Contains(bibDocExts, filepath.Ext(p)) {
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
			if e.IsDir() || !slices.Contains(bibDocExts, filepath.Ext(e.Name())) {
				return "", false
			}
			return filepath.Join(path, e.Name()), true
		})
	}
	slices.Sort(files)
	return files, nil
}
