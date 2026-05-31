package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/sciminds/cli/internal/duck"
	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	dbDryRun       bool
	dbAddTableName string
	dbDeleteYes    bool
	dbResetYes     bool
	dbRenameYes    bool
	dbInspectTable string
	dbHeadN        int
	dbTailN        int
	dbGlimpseN     int
	dbConvertAs    string
)

func dbCommand() *cli.Command {
	return &cli.Command{
		Name:        "db",
		Usage:       "Manage SQLite/DuckDB databases and tabular files",
		Description: "$ sci db add results.csv mydb.db\n$ sci db info project.duckdb\n$ sci db head data.parquet",
		Category:    "Commands",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without executing", Destination: &dbDryRun}, // lint:no-local — propagates to subcommands
		},
		Commands: []*cli.Command{
			dbCreateCommand(),
			dbResetCommand(),
			dbInfoCommand(),
			dbAddCommand(),
			dbAppendCommand(),
			dbDeleteCommand(),
			dbRenameCommand(),
			dbColsCommand(),
			dbHeadCommand(),
			dbTailCommand(),
			dbGlimpseCommand(),
			dbShapeCommand(),
			dbSummarizeCommand(),
			dbConvertCommand(),
			dbQueryCommand(),
			dbViewCommand(),
		},
	}
}

// duckTableFlag is the shared --table flag used by inspect verbs to pick
// a sqlite table, duckdb table, or xlsx sheet. The "sheet" alias makes
// xlsx-shaped UX feel natural.
func duckTableFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "table",
		Aliases:     []string{"t", "sheet"},
		Usage:       "table name (sqlite/duckdb) or sheet name (xlsx); required when ambiguous",
		Destination: &dbInspectTable,
		Local:       true,
	}
}

func dbColsCommand() *cli.Command {
	return &cli.Command{
		Name:        "cols",
		Usage:       "List column names and types of a tabular file",
		Description: "$ sci db cols data.csv\n$ sci db cols workbook.xlsx --table extras",
		ArgsUsage:   "<file>",
		Flags:       []cli.Flag{duckTableFlag()},
		Action:      duckInspectAction(func(path string) (any, error) { return duck.Cols(path, dbInspectTable) }),
	}
}

func dbHeadCommand() *cli.Command {
	return &cli.Command{
		Name:        "head",
		Usage:       "Show the first N rows of a tabular file",
		Description: "$ sci db head data.csv -n 20",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			duckTableFlag(),
			&cli.IntFlag{Name: "n", Usage: "number of rows", Value: 10, Destination: &dbHeadN, Local: true},
		},
		Action: duckInspectAction(func(path string) (any, error) { return duck.Head(path, dbInspectTable, dbHeadN) }),
	}
}

func dbTailCommand() *cli.Command {
	return &cli.Command{
		Name:        "tail",
		Usage:       "Show the last N rows of a tabular file",
		Description: "$ sci db tail data.csv -n 20",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			duckTableFlag(),
			&cli.IntFlag{Name: "n", Usage: "number of rows", Value: 10, Destination: &dbTailN, Local: true},
		},
		Action: duckInspectAction(func(path string) (any, error) { return duck.Tail(path, dbInspectTable, dbTailN) }),
	}
}

func dbGlimpseCommand() *cli.Command {
	return &cli.Command{
		Name:        "glimpse",
		Usage:       "Transposed preview: one row per column with sample values",
		Description: "$ sci db glimpse data.csv\n$ sci db glimpse data.parquet --samples 3",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			duckTableFlag(),
			&cli.IntFlag{Name: "samples", Usage: "values per column", Value: 5, Destination: &dbGlimpseN, Local: true},
		},
		Action: duckInspectAction(func(path string) (any, error) { return duck.Glimpse(path, dbInspectTable, dbGlimpseN) }),
	}
}

func dbShapeCommand() *cli.Command {
	return &cli.Command{
		Name:        "shape",
		Usage:       "Report (rows, cols) of a tabular file",
		Description: "$ sci db shape data.csv",
		ArgsUsage:   "<file>",
		Flags:       []cli.Flag{duckTableFlag()},
		Action:      duckInspectAction(func(path string) (any, error) { return duck.Shape(path, dbInspectTable) }),
	}
}

func dbSummarizeCommand() *cli.Command {
	return &cli.Command{
		Name:        "summarize",
		Usage:       "Per-column statistics (min/max/avg/std/quartiles/null %)",
		Description: "$ sci db summarize data.csv",
		ArgsUsage:   "<file>",
		Flags:       []cli.Flag{duckTableFlag()},
		Action:      duckInspectAction(func(path string) (any, error) { return duck.Summarize(path, dbInspectTable) }),
	}
}

func dbConvertCommand() *cli.Command {
	return &cli.Command{
		Name:  "convert",
		Usage: "Convert between csv/tsv/json/jsonl/parquet/sqlite/duckdb via duckdb",
		Description: "$ sci db convert data.csv data.parquet\n" +
			"$ sci db convert results.json results.csv\n" +
			"$ sci db convert data.csv archive.db --as observations\n" +
			"$ sci db convert source.db -t researchers source.duckdb\n" +
			"$ sci db convert source.duckdb -t events events.parquet",
		ArgsUsage: "<in> <out>",
		Flags: []cli.Flag{
			duckTableFlag(),
			&cli.StringFlag{
				Name:        "as",
				Usage:       "destination table name (sqlite/duckdb output only; defaults to source basename)",
				Destination: &dbConvertAs,
				Local:       true,
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 2 {
				return cmdutil.UsageErrorf(cmd, "expected 2 arguments: <in> <out>, got %d", len(args))
			}
			in, out := args[0], args[1]
			if dbDryRun {
				uikit.Hint(fmt.Sprintf("would convert %s → %s", in, out))
				return nil
			}
			if _, err := os.Stat(out); err == nil {
				return fmt.Errorf("output file already exists: %s (delete it first)", out)
			}
			result, err := duck.Convert(in, dbInspectTable, out, dbConvertAs)
			if err != nil {
				return wrapDuckErr(err)
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

// dbViewCommand mirrors the top-level `sci view` so users who reach for the
// `sci db` namespace can still find the interactive viewer. Both commands
// dispatch through viewAction.
func dbViewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Interactively browse a database or tabular file (same as `sci view`)",
		Description: "$ sci db view experiment.db\n$ sci db view data.csv\n$ sci db view lab.duckdb",
		ArgsUsage:   "<file>",
		Action:      viewAction,
	}
}

func dbQueryCommand() *cli.Command {
	return &cli.Command{
		Name:  "query",
		Usage: "Run a read-only SELECT against a file (databases: real table names; flat files: `src`)",
		Description: "$ sci db query data.csv 'SELECT name, score FROM src WHERE score > 2'\n" +
			"$ sci db query lab.duckdb 'SELECT title FROM documents LIMIT 5'",
		ArgsUsage: "<file> <sql>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 2 {
				return cmdutil.UsageErrorf(cmd, "expected 2 arguments: <file> <sql>, got %d", len(args))
			}
			result, err := duck.Query(args[0], args[1])
			if err != nil {
				return wrapDuckErr(err)
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

// duckInspectAction wraps the common shape of read-only inspect verbs:
// require one positional file arg, call run, render the result.
func duckInspectAction(run func(path string) (any, error)) cli.ActionFunc {
	return func(_ context.Context, cmd *cli.Command) error {
		if cmd.Args().Len() == 0 {
			return cmdutil.UsageErrorf(cmd, "expected a file argument")
		}
		raw, err := run(cmd.Args().First())
		if err != nil {
			return wrapDuckErr(err)
		}
		// Each verb returns a *Result that satisfies cmdutil.Result; we
		// type-assert here so the inspect verbs share one wrapper.
		result, ok := raw.(cmdutil.Result)
		if !ok {
			return fmt.Errorf("internal: verb returned unexpected type %T", raw)
		}
		cmdutil.Output(cmd, result)
		return nil
	}
}

// wrapDuckErr surfaces the optional-dep miss with a doctor pointer.
func wrapDuckErr(err error) error {
	if errors.Is(err, duck.ErrNotInstalled) {
		return err // its message already names `sci doctor`
	}
	return err
}

func dbCreateCommand() *cli.Command {
	return &cli.Command{
		Name:        "create",
		Usage:       "Create an empty database (SQLite or DuckDB by extension)",
		Description: "$ sci db create my-project.db\n$ sci db create lab.duckdb",
		ArgsUsage:   "<file>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file argument")
			}
			result, err := db.Create(cmd.Args().First())
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func dbResetCommand() *cli.Command {
	return &cli.Command{
		Name:        "reset",
		Usage:       "Delete and recreate an empty database (SQLite or DuckDB by extension)",
		Description: "$ sci db reset mydb.db\n$ sci db reset lab.duckdb",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &dbResetYes, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file argument")
			}
			dbPath := cmd.Args().First()
			if dbDryRun {
				uikit.Hint(fmt.Sprintf("would delete and recreate %s", dbPath))
				return nil
			}
			if done, err := cmdutil.ConfirmOrSkip(dbResetYes, fmt.Sprintf("This will delete all data in %s. Continue?", dbPath)); done || err != nil {
				return err
			}
			result, err := db.Reset(dbPath)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func dbInfoCommand() *cli.Command {
	return &cli.Command{
		Name:        "info",
		Usage:       "Show database metadata and tables",
		Description: "$ sci db info mydb.db\n$ sci db info lab.duckdb",
		ArgsUsage:   "<file>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a file argument")
			}
			result, err := db.Info(cmd.Args().First())
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func dbAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Import CSV files as new tables (errors if a table already exists — use `sci db append` to add rows)",
		Description: "$ sci db add results.csv mydb.db\n" +
			"$ sci db add results.csv lab.duckdb -t experiment_1",
		ArgsUsage: "<csv>... <database>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "table", Aliases: []string{"t"}, Usage: "override table name (single file only)", Destination: &dbAddTableName, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 2 {
				return cmdutil.UsageErrorf(cmd, "expected at least 2 arguments: <csv>... <database>")
			}
			dbPath := args[len(args)-1]
			csvFiles := args[:len(args)-1]
			if dbDryRun {
				for _, f := range csvFiles {
					tbl := dbAddTableName
					if tbl == "" {
						tbl = store.TableNameFromFile(f)
					}
					uikit.Hint(fmt.Sprintf("would import %s → table %q in %s", f, tbl, dbPath))
				}
				return nil
			}
			result, err := db.AddCSV(csvFiles, dbPath, dbAddTableName)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			if !cmdutil.IsJSON(cmd) {
				uikit.NextStep("sci view", "Explore your data interactively")
			}
			return nil
		},
	}
}

func dbAppendCommand() *cli.Command {
	return &cli.Command{
		Name:  "append",
		Usage: "Append CSV rows to existing tables (errors if the table is missing — use `sci db add` for new tables)",
		Description: "$ sci db append more_rows.csv mydb.db -t experiment_1\n" +
			"$ sci db append batch_*.csv lab.duckdb -t events",
		ArgsUsage: "<csv>... <database>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "table", Aliases: []string{"t"}, Usage: "override table name (single file only)", Destination: &dbAddTableName, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 2 {
				return cmdutil.UsageErrorf(cmd, "expected at least 2 arguments: <csv>... <database>")
			}
			dbPath := args[len(args)-1]
			csvFiles := args[:len(args)-1]
			if dbDryRun {
				for _, f := range csvFiles {
					tbl := dbAddTableName
					if tbl == "" {
						tbl = store.TableNameFromFile(f)
					}
					uikit.Hint(fmt.Sprintf("would append %s → table %q in %s", f, tbl, dbPath))
				}
				return nil
			}
			result, err := db.AppendCSV(csvFiles, dbPath, dbAddTableName)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func dbDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Usage:       "Delete a table or view from a database",
		Description: "$ sci db delete old_experiments mydb.db\n$ sci db delete vec_cache lab.duckdb",
		ArgsUsage:   "<table> <database>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &dbDeleteYes, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 2 {
				return cmdutil.UsageErrorf(cmd, "expected 2 arguments, got %d", len(args))
			}
			table := args[0]
			dbPath := args[1]
			if dbDryRun {
				uikit.Hint(fmt.Sprintf("would delete table %q from %s", table, dbPath))
				return nil
			}
			if done, err := cmdutil.ConfirmOrSkip(dbDeleteYes, fmt.Sprintf("Drop table %q from %s?", table, dbPath)); done || err != nil {
				return err
			}
			result, err := db.DeleteTable(table, dbPath)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func dbRenameCommand() *cli.Command {
	return &cli.Command{
		Name:        "rename",
		Usage:       "Rename a table or view in a database",
		Description: "$ sci db rename raw_data cleaned_data mydb.db\n$ sci db rename documents articles lab.duckdb",
		ArgsUsage:   "<old> <new> <database>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &dbRenameYes, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 3 {
				return cmdutil.UsageErrorf(cmd, "expected 3 arguments, got %d", len(args))
			}
			oldName, newName := args[0], args[1]
			dbPath := args[2]
			if dbDryRun {
				uikit.Hint(fmt.Sprintf("would rename table %q → %q in %s", oldName, newName, dbPath))
				return nil
			}
			if done, err := cmdutil.ConfirmOrSkip(dbRenameYes, fmt.Sprintf("Rename table %q to %q in %s?", oldName, newName, dbPath)); done || err != nil {
				return err
			}
			result, err := db.RenameTable(oldName, newName, dbPath)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}
