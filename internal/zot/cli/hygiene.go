package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/urfave/cli/v3"
)

// Flag destinations for hygiene commands.
var (
	missingFields string
	missingLimit  int

	dupStrategy  string
	dupThreshold float64
	dupLimit     int

	invalidFields string
	invalidLimit  int
)

func missingCommand() *cli.Command {
	return &cli.Command{
		Name:  "missing",
		Usage: "Scan the library for items missing common fields",
		Description: `$ zot missing
$ zot missing --field title,creators
$ zot missing --field doi,abstract
$ zot missing --limit 0 --json > coverage.json

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
		Usage: "Find potential duplicate items (by DOI and/or title similarity)",
		Description: `$ zot duplicates
$ zot duplicates --strategy doi
$ zot duplicates --strategy title --threshold 0.9
$ zot duplicates --limit 0 --json > dupes.json

Strategies: doi (strongest), title (exact normalized + fuzzy), both (default).
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
			&cli.FloatFlag{
				Name:        "threshold",
				Aliases:     []string{"t"},
				Value:       0.85,
				Usage:       "fuzzy title similarity floor (0..1)",
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
		Description: `$ zot invalid
$ zot invalid --field doi,date
$ zot invalid --limit 0 --json > invalid.json

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
