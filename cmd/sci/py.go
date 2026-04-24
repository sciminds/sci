package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/py"
	"github.com/urfave/cli/v3"
)

var pyREPLWithPkgs string
var pyREPLIgnoreExisting bool

var pyNotebookWithPkgs string
var pyNotebookIgnoreExisting bool

func pyCommand() *cli.Command {
	return &cli.Command{
		Name:        "py",
		Usage:       "Create/launch quick Python scratchpads and notebooks",
		Description: "$ sci py repl\n$ sci py notebook",
		Category:    "Commands",
		Commands: []*cli.Command{
			pyREPLCommand(),
			pyNotebookCommand(),
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
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Destination: &pyREPLWithPkgs, Local: true},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Destination: &pyREPLIgnoreExisting, Local: true},
		},
		Action: runPyREPL,
	}
}

func pyNotebookCommand() *cli.Command {
	return &cli.Command{
		Name:        "notebook",
		Usage:       "Open a marimo notebook",
		Description: "$ sci py notebook\n$ sci py notebook --with pandas,matplotlib\n$ sci py notebook --ignore-existing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Destination: &pyNotebookWithPkgs, Local: true},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Destination: &pyNotebookIgnoreExisting, Local: true},
		},
		Action: runPyNotebook,
	}
}

func runPyREPL(_ context.Context, _ *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.IPythonTool, parsePkgs(pyREPLWithPkgs), pyREPLIgnoreExisting)
}

func runPyNotebook(_ context.Context, _ *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.MarimoTool, parsePkgs(pyNotebookWithPkgs), pyNotebookIgnoreExisting)
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
