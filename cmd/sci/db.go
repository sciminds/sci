package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/db"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	dbDryRun       bool
	dbAddTableName string
	dbDeleteYes    bool
	dbResetYes     bool
	dbRenameYes    bool
)

func dbCommand() *cli.Command {
	return &cli.Command{
		Name:        "db",
		Usage:       "Manage SQLite databases and spreadsheets",
		Description: "$ sci db add results.csv mydb.db\n$ sci db info mydb.db",
		Category:    "Commands",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without executing", Destination: &dbDryRun}, // lint:no-local — propagates to subcommands
		},
		Commands: []*cli.Command{
			dbCreateCommand(),
			dbResetCommand(),
			dbInfoCommand(),
			dbAddCommand(),
			dbDeleteCommand(),
			dbRenameCommand(),
		},
	}
}

func dbCreateCommand() *cli.Command {
	return &cli.Command{
		Name:        "create",
		Usage:       "Create an empty database",
		Description: "$ sci db create my-project.db",
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
		Usage:       "Delete and recreate an empty database",
		Description: "$ sci db reset mydb.db",
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
				ui.Hint(fmt.Sprintf("would delete and recreate %s", dbPath))
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
		Description: "$ sci db info mydb.db",
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
		Name:        "add",
		Usage:       "Import CSV files into a database",
		Description: "$ sci db add results.csv mydb.db\n$ sci db add results.csv mydb.db -t experiment_1",
		ArgsUsage:   "<csv>... <database>",
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
						tbl = strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
					}
					ui.Hint(fmt.Sprintf("would import %s → table %q in %s", f, tbl, dbPath))
				}
				return nil
			}
			result, err := db.AddCSV(csvFiles, dbPath, dbAddTableName)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			if !cmdutil.IsJSON(cmd) {
				ui.NextStep("sci view", "Explore your data interactively")
			}
			return nil
		},
	}
}

func dbDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Usage:       "Delete a table from a database",
		Description: "$ sci db delete old_experiments mydb.db",
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
				ui.Hint(fmt.Sprintf("would delete table %q from %s", table, dbPath))
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
		Usage:       "Rename a table in a database",
		Description: "$ sci db rename raw_data cleaned_data mydb.db",
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
				ui.Hint(fmt.Sprintf("would rename table %q → %q in %s", oldName, newName, dbPath))
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
