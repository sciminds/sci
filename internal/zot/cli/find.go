package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/openalex"
	"github.com/urfave/cli/v3"
)

var (
	findPerPage int
	findCursor  string
	findSort    string
	findFilters []string
	findVerbose bool
)

func findCommand() *cli.Command {
	return &cli.Command{
		Name:  "find",
		Usage: "Look up papers or authors on OpenAlex",
		Description: "$ sci zot find works \"attention is all you need\"\n" +
			"$ sci zot find authors \"ashish vaswani\"\n" +
			"$ sci zot find works --filter type=article --filter from_publication_date=2024-01-01 llm",
		Commands: []*cli.Command{
			findWorksCommand(),
			findAuthorsCommand(),
		},
	}
}

func findFlags() []cli.Flag {
	return []cli.Flag{
		// `--limit` is the canonical name (mirrors `search --limit` and
		// `item list --limit`). `--per-page` stays as an alias to keep
		// older scripts working — they alias at the flag layer so both
		// populate findPerPage transparently.
		&cli.IntFlag{Name: "limit", Aliases: []string{"n", "per-page"}, Value: 25, Usage: "max results per page (1-200)", Destination: &findPerPage, Local: true},
		&cli.StringFlag{Name: "cursor", Usage: "continuation cursor from a previous page", Destination: &findCursor, Local: true},
		&cli.StringFlag{Name: "sort", Usage: "sort expression, e.g. cited_by_count:desc", Destination: &findSort, Local: true},
		&cli.StringSliceFlag{Name: "filter", Usage: "OpenAlex filter as key=value (repeatable)", Destination: &findFilters}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
		&cli.BoolFlag{Name: "verbose", Usage: "--json: emit the full raw OpenAlex records (default is a compact per-work shape)", Destination: &findVerbose, Local: true},
	}
}

func findWorksCommand() *cli.Command {
	return &cli.Command{
		Name:      "works",
		Usage:     "Search OpenAlex Works",
		ArgsUsage: "<query>",
		Description: "$ sci zot find works \"attention is all you need\"\n" +
			"$ sci zot find works --limit 10 --sort cited_by_count:desc transformers\n" +
			"$ sci zot find works --filter type=article --filter from_publication_date=2024-01-01 llm",
		Flags:  findFlags(),
		Action: runFindWorks,
	}
}

func findAuthorsCommand() *cli.Command {
	return &cli.Command{
		Name:      "authors",
		Usage:     "Search OpenAlex Authors",
		ArgsUsage: "<query>",
		Description: "$ sci zot find authors \"ashish vaswani\"\n" +
			"$ sci zot find authors --sort works_count:desc hinton",
		Flags:  findFlags(),
		Action: runFindAuthors,
	}
}

// Default field masks — trim huge sub-objects (abstract_inverted_index,
// x_concepts, counts_by_year) so `zot find` stays skim-friendly even in
// --json. Callers that want the full record can pass --select.
var (
	worksSelectFields   = []string{"id", "doi", "title", "display_name", "publication_year", "publication_date", "type", "authorships", "primary_location", "cited_by_count", "open_access", "best_oa_location"}
	authorsSelectFields = []string{"id", "orcid", "display_name", "works_count", "cited_by_count", "summary_stats", "last_known_institutions"}
)

func runFindWorks(ctx context.Context, cmd *cli.Command) error {
	query, opts, err := parseFindArgs(cmd, worksSelectFields)
	if err != nil {
		return err
	}
	client, err := openalexClient()
	if err != nil {
		return err
	}
	res, err := client.SearchWorks(ctx, opts)
	if err != nil {
		return err
	}
	out := zot.FindWorksResult{
		Query:   query,
		Total:   res.Meta.Count,
		Count:   len(res.Results),
		Works:   res.Results,
		Verbose: findVerbose,
	}
	if res.Meta.NextCursor != nil {
		out.NextCursor = *res.Meta.NextCursor
	}
	cmdutil.Output(cmd, out)
	return nil
}

func runFindAuthors(ctx context.Context, cmd *cli.Command) error {
	query, opts, err := parseFindArgs(cmd, authorsSelectFields)
	if err != nil {
		return err
	}
	client, err := openalexClient()
	if err != nil {
		return err
	}
	res, err := client.SearchAuthors(ctx, opts)
	if err != nil {
		return err
	}
	out := zot.FindAuthorsResult{
		Query:   query,
		Total:   res.Meta.Count,
		Count:   len(res.Results),
		Authors: res.Results,
		Verbose: findVerbose,
	}
	if res.Meta.NextCursor != nil {
		out.NextCursor = *res.Meta.NextCursor
	}
	cmdutil.Output(cmd, out)
	return nil
}

// parseFindArgs pulls the positional query out of the CLI args and merges the
// shared find flags into a SearchOpts. selectFields is the entity-specific
// default field mask — /works and /authors accept disjoint names.
func parseFindArgs(cmd *cli.Command, selectFields []string) (string, openalex.SearchOpts, error) {
	query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
	if query == "" {
		return "", openalex.SearchOpts{}, fmt.Errorf("query is required")
	}
	filter, err := parseFilters(findFilters)
	if err != nil {
		return "", openalex.SearchOpts{}, err
	}
	return query, openalex.SearchOpts{
		Search:  query,
		Filter:  filter,
		PerPage: findPerPage,
		Cursor:  findCursor,
		Sort:    findSort,
		Select:  selectFields,
	}, nil
}

func parseFilters(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, pair := range raw {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("filter must be key=value, got %q", pair)
		}
		out[k] = v
	}
	return out, nil
}

// openalexClient builds an OpenAlex HTTP client from saved config. Both
// credentials are optional — an empty config just falls through to the
// anonymous tier.
func openalexClient() (*openalex.Client, error) {
	cfg, err := zot.LoadConfig()
	if err != nil {
		return nil, err
	}
	email, apiKey := "", ""
	if cfg != nil {
		email, apiKey = cfg.OpenAlexEmail, cfg.OpenAlexAPIKey
	}
	return openalex.NewClient(email, apiKey), nil
}
