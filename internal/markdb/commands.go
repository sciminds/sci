package markdb

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/urfave/cli/v3"
)

var dbFlag = &cli.StringFlag{
	Name:     "db",
	Usage:    "path to SQLite database",
	Required: true,
	Local:    true,
}

// BuildCommand returns the top-level markdb CLI command.
func BuildCommand(jsonOutput *bool) *cli.Command {
	return &cli.Command{
		Name:    "markdb",
		Usage:   "Markdown-to-SQLite ingestion, search, and export",
		Version: "0.1.0",
		Flags: []cli.Flag{
			jsonFlag(jsonOutput),
		},
		Commands: []*cli.Command{
			ingestCommand(),
			searchCommand(),
			infoCommand(),
			diffCommand(),
			exportCommand(),
		},
	}
}

func openStore(cmd *cli.Command) (*Store, error) {
	dbPath := cmd.String("db")
	s, err := Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := s.InitSchema(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

const defaultDB = "mark.db"

func ingestCommand() *cli.Command {
	return &cli.Command{
		Name:      "ingest",
		Usage:     "Ingest a directory of markdown files into the database",
		ArgsUsage: "<dir>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "path to output database (default: ./mark.db)",
				Local:   true,
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return usageErrorf(cmd, "expected a directory argument")
			}

			dbPath := cmd.String("output")
			if dbPath == "" {
				dbPath = defaultDB
			}

			s, err := Open(dbPath)
			if err != nil {
				return err
			}
			if err := s.InitSchema(); err != nil {
				_ = s.Close()
				return err
			}
			defer func() { _ = s.Close() }()

			dir := cmd.Args().First()

			if isJSON(cmd) || !term.IsTerminal(os.Stdout.Fd()) {
				// No progress bar in JSON mode or non-interactive terminals.
				stats, err := s.Ingest(dir)
				if err != nil {
					return err
				}
				resolved, broken, err := s.ResolveLinks()
				if err != nil {
					return fmt.Errorf("resolve links: %w", err)
				}
				result := IngestCmdResult{Stats: *stats, DB: dbPath}
				result.Links.Resolved = resolved
				result.Links.Broken = broken
				output(cmd, result)
				return nil
			}

			result, err := RunIngestWithProgress(s, dir, dbPath)
			if err != nil {
				return err
			}
			output(cmd, result)
			return nil
		},
	}
}

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "Full-text search across files",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			dbFlag,
			&cli.IntFlag{Name: "limit", Usage: "max results", Value: 50, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return usageErrorf(cmd, "expected a search query")
			}
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			hits, err := s.Search(cmd.Args().First(), int(cmd.Int("limit")))
			if err != nil {
				return err
			}
			output(cmd, SearchCmdResult{Query: cmd.Args().First(), Hits: hits})
			return nil
		},
	}
}

func infoCommand() *cli.Command {
	return &cli.Command{
		Name:  "info",
		Usage: "Show database summary statistics",
		Flags: []cli.Flag{dbFlag},
		Action: func(_ context.Context, cmd *cli.Command) error {
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			info, err := s.Info()
			if err != nil {
				return err
			}
			output(cmd, info)
			return nil
		},
	}
}

func diffCommand() *cli.Command {
	return &cli.Command{
		Name:      "diff",
		Usage:     "Show what would change on next ingest",
		ArgsUsage: "<dir>",
		Flags:     []cli.Flag{dbFlag},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return usageErrorf(cmd, "expected a directory argument")
			}
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			result, err := s.Diff(cmd.Args().First())
			if err != nil {
				return err
			}
			output(cmd, DiffCmdResult{Result: *result})
			return nil
		},
	}
}

func exportCommand() *cli.Command {
	var where string
	var dir string
	return &cli.Command{
		Name:  "export",
		Usage: "Reconstruct markdown files from the database",
		Flags: []cli.Flag{
			dbFlag,
			&cli.StringFlag{Name: "dir", Usage: "output directory", Destination: &dir, Required: true, Local: true},
			&cli.StringFlag{Name: "where", Usage: "SQL WHERE clause to filter files", Destination: &where, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			stats, err := s.Export(dir, where)
			if err != nil {
				return err
			}
			output(cmd, ExportCmdResult{Stats: *stats})
			return nil
		},
	}
}
