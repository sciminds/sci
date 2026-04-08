package main

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	brewFile    string
	brewDryRun  bool
	brewCask    bool
	brewTap     bool
	brewUv      bool
	brewGo      bool
	brewCargo   bool
	brewFormula bool
	brewAll     bool
	brewCheck   bool
)

func brewCommand() *cli.Command {
	return &cli.Command{
		Name:        "brew",
		Usage:       "Keeps Homebrew in-sync with your Brewfile",
		Description: "$ sci brew install\n$ sci brew install pandoc\n$ sci brew uninstall pandoc\n$ sci brew list\n$ sci brew update\n$ sci brew update --check",
		Category:    "Maintenance",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "file",
				Usage:       "path to Brewfile",
				Value:       brew.DefaultBrewfile,
				Destination: &brewFile,
			},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without executing", Destination: &brewDryRun},
		},
		Commands: []*cli.Command{
			brewInstallCommand(),
			brewUninstallCommand(),
			brewListCommand(),
			brewUpdateCommand(),
		},
	}
}

func brewInstallCommand() *cli.Command {
	return &cli.Command{
		Name:        "install",
		Usage:       "Install packages from the Brewfile, or add and install a new package",
		Description: "$ sci brew install\n$ sci brew install pandoc\n$ sci brew install --cask firefox",
		ArgsUsage:   "[package]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "cask", Usage: "add as a cask", Destination: &brewCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "add as a tap", Destination: &brewTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "add as a uv tool", Destination: &brewUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "add as a Go package", Destination: &brewGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "add as a Cargo package", Destination: &brewCargo, Local: true},
		},
		Action: runBrewInstall,
	}
}

func brewUninstallCommand() *cli.Command {
	return &cli.Command{
		Name:        "uninstall",
		Usage:       "Remove a package from the Brewfile and uninstall it",
		Description: "$ sci brew uninstall pandoc\n$ sci brew uninstall --cask firefox",
		ArgsUsage:   "<package>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "formula", Usage: "remove a formula", Destination: &brewFormula, Local: true},
			&cli.BoolFlag{Name: "cask", Usage: "remove a cask", Destination: &brewCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "remove a tap", Destination: &brewTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "remove a uv tool", Destination: &brewUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "remove a Go package", Destination: &brewGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "remove a Cargo package", Destination: &brewCargo, Local: true},
		},
		Action: runBrewUninstall,
	}
}

func brewListCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Usage:       "List packages in the Brewfile",
		Description: "$ sci brew list\n$ sci brew list --cask",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "formula", Usage: "list formulae only", Destination: &brewFormula, Local: true},
			&cli.BoolFlag{Name: "cask", Usage: "list casks only", Destination: &brewCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "list taps only", Destination: &brewTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "list uv tools only", Destination: &brewUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "list Go packages only", Destination: &brewGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "list Cargo packages only", Destination: &brewCargo, Local: true},
			&cli.BoolFlag{Name: "all", Usage: "list all package types", Destination: &brewAll, Local: true},
		},
		Action: runBrewList,
	}
}

func resolveBrewfile() (string, error) {
	return brew.ExpandPath(brewFile)
}

func resolvePkgType() string {
	switch {
	case brewCask:
		return "cask"
	case brewTap:
		return "tap"
	case brewUv:
		return "uv"
	case brewGo:
		return "go"
	case brewCargo:
		return "cargo"
	case brewFormula:
		return "formula"
	default:
		return ""
	}
}

func runBrewInstall(_ context.Context, cmd *cli.Command) error {
	file, err := resolveBrewfile()
	if err != nil {
		return err
	}

	// With a package argument: add to Brewfile and install.
	if cmd.NArg() > 0 {
		pkg := cmd.Args().First()
		pkgType := resolvePkgType()

		if brewDryRun {
			label := "formula"
			if pkgType != "" {
				label = pkgType
			}
			ui.Hint(fmt.Sprintf("would add %s (%s) to %s", pkg, label, file))
			return nil
		}

		var result brew.AddResult
		err = ui.RunWithSpinner(fmt.Sprintf("Adding %s…", pkg), func(_, _ func(string)) error {
			var addErr error
			result, addErr = brew.Add(brew.BundleRunner{}, file, pkg, pkgType)
			return addErr
		})
		if err != nil {
			return err
		}

		cmdutil.Output(cmd, result)
		return nil
	}

	// No arguments: sync all packages from the Brewfile.
	if brewDryRun {
		ui.Hint(fmt.Sprintf("would install all packages from %s", file))
		return nil
	}

	var result brew.InstallResult
	err = ui.RunWithSpinner("Installing from Brewfile…", func(_, _ func(string)) error {
		var instErr error
		result, instErr = brew.Install(brew.BundleRunner{}, file)
		return instErr
	})
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)
	return nil
}

func runBrewUninstall(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return cmdutil.UsageErrorf(cmd, "expected a package name")
	}
	file, err := resolveBrewfile()
	if err != nil {
		return err
	}
	pkg := cmd.Args().First()

	if brewDryRun {
		ui.Hint(fmt.Sprintf("would remove %s from %s", pkg, file))
		return nil
	}

	var result brew.RemoveResult
	err = ui.RunWithSpinner(fmt.Sprintf("Removing %s…", pkg), func(_, _ func(string)) error {
		var rmErr error
		result, rmErr = brew.Remove(brew.BundleRunner{}, file, pkg, resolvePkgType())
		return rmErr
	})
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)
	return nil
}

func runBrewList(_ context.Context, cmd *cli.Command) error {
	file, err := resolveBrewfile()
	if err != nil {
		return err
	}
	runner := brew.BundleRunner{}

	// Type-specific filter or --json: plain text list.
	pkgType := resolvePkgType()
	if pkgType != "" || cmdutil.IsJSON(cmd) {
		if brewAll {
			pkgType = ""
		}
		result, listErr := brew.List(runner, file, pkgType)
		if listErr != nil {
			return listErr
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	// Default: interactive TUI with descriptions.
	var packages []brew.PackageInfo
	err = ui.RunWithSpinner("Loading package info…", func(_, _ func(string)) error {
		var detErr error
		packages, detErr = brew.ListDetailed(runner, file)
		return detErr
	})
	if err != nil {
		return err
	}

	return brew.RunListTUI(packages)
}

func brewUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:        "update",
		Usage:       "Update the Homebrew registry and upgrade outdated packages",
		Description: "$ sci brew update\n$ sci brew update --check",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "check",
				Usage:       "only show outdated packages without upgrading",
				Destination: &brewCheck,
				Local:       true,
			},
		},
		Action: runBrewUpdate,
	}
}

func runBrewUpdate(_ context.Context, cmd *cli.Command) error {
	runner := brew.BundleRunner{}

	if brewDryRun {
		if brewCheck {
			ui.Hint("would update the registry and list outdated packages")
		} else {
			ui.Hint("would update the registry and upgrade outdated packages")
		}
		return nil
	}

	var result brew.UpdateResult
	err := ui.RunWithSpinner("Updating package registry…", func(setTitle, setStatus func(string)) error {
		var updateErr error
		result, updateErr = brew.Update(runner, brewCheck, setTitle, setStatus)
		return updateErr
	})
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)
	return nil
}
