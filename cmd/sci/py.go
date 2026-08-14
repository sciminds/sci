package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/sci/internal/py"
	"github.com/urfave/cli/v3"
)

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
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Local: true},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Local: true},
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
			&cli.StringFlag{Name: "with", Usage: "extra packages (comma-separated)", Local: true},
			&cli.BoolFlag{Name: "ignore-existing", Usage: "skip environment detection, use ephemeral", Local: true},
		},
		Action: runPyNotebook,
	}
}

func runPyREPL(_ context.Context, cmd *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.IPythonTool, parsePkgs(cmd.String("with")), cmd.Bool("ignore-existing"))
}

func runPyNotebook(_ context.Context, cmd *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	return py.RunTool(dir, py.MarimoTool, parsePkgs(cmd.String("with")), cmd.Bool("ignore-existing"))
}

func parsePkgs(csv string) []string {
	if csv == "" {
		return nil
	}
	var pkgs []string
	for pkg := range strings.SplitSeq(csv, ",") {
		if p := strings.TrimSpace(pkg); p != "" {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs
}
