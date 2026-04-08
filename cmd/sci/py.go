package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/py"
	"github.com/sciminds/cli/internal/py/tutorials"
	"github.com/urfave/cli/v3"
)

var pyREPLWithPkgs string
var pyREPLIgnoreExisting bool

var pyMarimoWithPkgs string
var pyMarimoIgnoreExisting bool

var pyTutorialsName string
var pyTutorialsAll bool
var pyTutorialsWithData bool

func pyCommand() *cli.Command {
	return &cli.Command{
		Name:        "py",
		Usage:       "Create/launch quick Python scratchpads and notebooks",
		Description: "$ sci py repl\n$ sci py tutorials",
		Category:    "Commands",
		Commands: []*cli.Command{
			pyREPLCommand(),
			pyMarimoCommand(),
			pyTutorialsCommand(),
			convertCommand(),
		},
	}
}

func pyREPLCommand() *cli.Command {
	return &cli.Command{
		Name:        "repl",
		Usage:       "Open a Python scratchpad",
		Description: "$ sci py repl\n$ sci py repl --with pandas,matplotlib\n$ sci py repl --ignore-existing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Destination: &pyREPLWithPkgs},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Destination: &pyREPLIgnoreExisting},
		},
		Action: runPyREPL,
	}
}

func pyMarimoCommand() *cli.Command {
	return &cli.Command{
		Name:        "marimo",
		Usage:       "Open a marimo notebook",
		Description: "$ sci py marimo\n$ sci py marimo --with pandas,matplotlib\n$ sci py marimo --ignore-existing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Destination: &pyMarimoWithPkgs},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Destination: &pyMarimoIgnoreExisting},
		},
		Action: runPyMarimo,
	}
}

func pyTutorialsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tutorials",
		Usage:       "Browse and run tutorial notebooks in marimo",
		Description: "$ sci py tutorials\n$ sci py tutorials --name 08-categorical-coding\n$ sci py tutorials --all --with-data",
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
			{
				Flags: [][]cli.Flag{
					{&cli.StringFlag{Name: "name", Usage: "download a specific tutorial by name", Destination: &pyTutorialsName}},
					{&cli.BoolFlag{Name: "all", Usage: "download all tutorials", Destination: &pyTutorialsAll}},
				},
			},
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "with-data", Usage: "also download data, figures, and helpers", Destination: &pyTutorialsWithData, Local: true},
		},
		Action: runPyTutorials,
	}
}

func runPyREPL(_ context.Context, _ *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.IPythonTool, parsePkgs(pyREPLWithPkgs), pyREPLIgnoreExisting)
}

func runPyMarimo(_ context.Context, _ *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.MarimoTool, parsePkgs(pyMarimoWithPkgs), pyMarimoIgnoreExisting)
}

func parsePkgs(csv string) []string {
	if csv == "" {
		return nil
	}
	var pkgs []string
	for _, pkg := range strings.Split(csv, ",") {
		if p := strings.TrimSpace(pkg); p != "" {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs
}

func runPyTutorials(_ context.Context, _ *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}

	if pyTutorialsAll {
		if pyTutorialsWithData {
			return tutorials.FetchAllWithAssets(dir)
		}
		return tutorials.FetchAll(dir)
	}

	if pyTutorialsName != "" {
		names := []string{pyTutorialsName}
		if pyTutorialsWithData {
			return tutorials.FetchWithAssets(names, dir)
		}
		return tutorials.Fetch(names, dir)
	}

	// No flags: interactive picker
	selected, err := tutorials.RunSelect()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		return err
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "no tutorials selected")
		return nil
	}
	if pyTutorialsWithData {
		return tutorials.FetchWithAssets(selected, dir)
	}
	return tutorials.Fetch(selected, dir)
}
