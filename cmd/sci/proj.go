package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/proj"
	projnew "github.com/sciminds/cli/internal/proj/new"
	configTUI "github.com/sciminds/cli/internal/proj/new/tui"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	projNewPkgManager string
	projNewDocSystem  string
	projNewAuthor     string
	projNewEmail      string
	projNewDesc       string
	projNewDryRun     bool
	projConfigDryRun  bool
)

func projCommand() *cli.Command {
	return &cli.Command{
		Name:        "proj",
		Usage:       "Quickly setup common projects (Python data-analysis, Webapps)",
		Description: "$ sci proj new\n$ sci proj add pandas",
		Category:    "Commands",
		Commands: []*cli.Command{
			projNewCommand(),
			projConfigCommand(),
			projAddCommand(),
			projRemoveCommand(),
			projRunCommand(),
			projRenderCommand(),
			projPreviewCommand(),
		},
	}
}

func projNewCommand() *cli.Command {
	return &cli.Command{
		Name:        "new",
		Usage:       "Create a new Python project",
		Description: "$ sci proj new\n$ sci proj new my-analysis --pkg-manager pixi",
		ArgsUsage:   "[name]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "pkg-manager", Usage: "package manager (pixi or uv)", Destination: &projNewPkgManager, Local: true},
			&cli.StringFlag{Name: "doc-system", Usage: "doc system (quarto, myst, or none)", Destination: &projNewDocSystem, Local: true},
			&cli.StringFlag{Name: "author", Usage: "author name", Destination: &projNewAuthor, Local: true},
			&cli.StringFlag{Name: "email", Usage: "author email", Destination: &projNewEmail, Local: true},
			&cli.StringFlag{Name: "description", Usage: "project description", Destination: &projNewDesc, Local: true},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be created without writing", Destination: &projNewDryRun, Local: true},
		},
		Action: runProjNew,
	}
}

func projConfigCommand() *cli.Command {
	return &cli.Command{
		Name:        "config",
		Usage:       "Refresh config files in your project",
		Description: "$ sci proj config\n$ sci proj config --dry-run",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show changes without applying", Destination: &projConfigDryRun, Local: true},
		},
		Action: runProjConfig,
	}
}

func projAddCommand() *cli.Command {
	return &cli.Command{
		Name:        "add",
		Usage:       "Add packages to the project",
		Description: "$ sci proj add pandas\n$ sci proj add numpy matplotlib seaborn",
		ArgsUsage:   "<pkg>...",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least 1 argument")
			}
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return proj.Add(dir, cmd.Args().Slice())
		},
	}
}

func projRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:        "remove",
		Usage:       "Remove packages from the project",
		Description: "$ sci proj remove pandas",
		ArgsUsage:   "<pkg>...",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least 1 argument")
			}
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return proj.Remove(dir, cmd.Args().Slice())
		},
	}
}

func projRunCommand() *cli.Command {
	return &cli.Command{
		Name:            "run",
		Usage:           "Run a project task",
		Description:     "$ sci proj run test\n$ sci proj run lint --fix",
		ArgsUsage:       "<task> [args...]",
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least 1 argument")
			}
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return proj.RunTask(dir, args[0], args[1:])
		},
	}
}

func projRenderCommand() *cli.Command {
	return &cli.Command{
		Name:        "render",
		Usage:       "Build documents into HTML or PDF",
		Description: "$ sci proj render\n$ sci proj render report.qmd",
		ArgsUsage:   "[target]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			target := ""
			if cmd.Args().Len() > 0 {
				target = cmd.Args().First()
			}
			return proj.Render(dir, target)
		},
	}
}

func projPreviewCommand() *cli.Command {
	return &cli.Command{
		Name:        "preview",
		Usage:       "Start a live preview server for documents",
		Description: "$ sci proj preview",
		Action: func(_ context.Context, _ *cli.Command) error {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return proj.Preview(dir)
		},
	}
}

func runProjNew(_ context.Context, cmd *cli.Command) error {
	opts := projnew.CreateOptions{
		Dir:         ".",
		PkgManager:  projNewPkgManager,
		DocSystem:   projNewDocSystem,
		AuthorName:  projNewAuthor,
		AuthorEmail: projNewEmail,
		Description: projNewDesc,
		DryRun:      projNewDryRun,
	}

	if cmd.Args().Len() > 0 {
		opts.Name = cmd.Args().First()
		opts.PkgManager = cmp.Or(opts.PkgManager, "uv")
		opts.DocSystem = cmp.Or(opts.DocSystem, "myst")
	} else if cmdutil.IsJSON(cmd) {
		return fmt.Errorf("project name argument is required in --json mode")
	} else {
		if err := projnew.RunWizard(&opts); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return nil
			}
			return err
		}
	}

	result, err := projnew.Create(opts)
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, *result)
	if !cmdutil.IsJSON(cmd) {
		ui.NextStep("cd "+opts.Name+" && sci py repl", "Jump into your new project")
	}
	return nil
}

func runProjConfig(_ context.Context, cmd *cli.Command) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}

	if cmdutil.IsJSON(cmd) || projConfigDryRun {
		result, err := projnew.Sync(dir, projConfigDryRun)
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, *result)
		return nil
	}

	files, err := projnew.PlanConfig(dir)
	if err != nil {
		return err
	}

	model := configTUI.New(configTUI.Options{Dir: dir, Files: files})
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	ui.DrainStdin()
	if err != nil {
		return fmt.Errorf("config TUI: %w", err)
	}
	m, ok := finalModel.(configTUI.Model)
	if !ok {
		return fmt.Errorf("config TUI: unexpected model type %T", finalModel)
	}
	if m.Err != nil {
		return m.Err
	}
	if len(m.Result.Changed) > 0 {
		cmdutil.Output(cmd, m.Result)
	}
	return nil
}
