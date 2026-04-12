package main

import (
	"context"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/doctor"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	toolsFile    string
	toolsDryRun  bool
	toolsCask    bool
	toolsTap     bool
	toolsUv      bool
	toolsGo      bool
	toolsCargo   bool
	toolsFormula bool
	toolsAll     bool
)

func toolsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tools",
		Usage:       "Manage Homebrew & uv tools via your Brewfile",
		Description: "$ sci tools install\n$ sci tools install pandoc\n$ sci tools uninstall pandoc\n$ sci tools list\n$ sci tools update\n$ sci tools outdated\n$ sci tools reccs",
		Category:    "Maintenance",
		Flags: []cli.Flag{
			// lint:no-local — propagates to subcommands
			&cli.StringFlag{
				Name:        "file",
				Usage:       "path to Brewfile",
				Value:       brew.DefaultBrewfile,
				Destination: &toolsFile,
			},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without executing", Destination: &toolsDryRun}, // lint:no-local
		},
		Commands: []*cli.Command{
			toolsInstallCommand(),
			toolsUninstallCommand(),
			toolsListCommand(),
			toolsUpdateCommand(),
			toolsOutdatedCommand(),
			toolsReccsCommand(),
		},
	}
}

func toolsInstallCommand() *cli.Command {
	return &cli.Command{
		Name:        "install",
		Usage:       "Install packages from the Brewfile, or add and install a new package",
		Description: "$ sci tools install\n$ sci tools install pandoc\n$ sci tools install --cask firefox",
		ArgsUsage:   "[package]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "cask", Usage: "add as a cask", Destination: &toolsCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "add as a tap", Destination: &toolsTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "add as a uv tool", Destination: &toolsUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "add as a Go package", Destination: &toolsGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "add as a Cargo package", Destination: &toolsCargo, Local: true},
		},
		Action: runToolsInstall,
	}
}

func toolsUninstallCommand() *cli.Command {
	return &cli.Command{
		Name:        "uninstall",
		Usage:       "Remove a package from the Brewfile and uninstall it",
		Description: "$ sci tools uninstall pandoc\n$ sci tools uninstall --cask firefox",
		ArgsUsage:   "<package>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "formula", Usage: "remove a formula", Destination: &toolsFormula, Local: true},
			&cli.BoolFlag{Name: "cask", Usage: "remove a cask", Destination: &toolsCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "remove a tap", Destination: &toolsTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "remove a uv tool", Destination: &toolsUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "remove a Go package", Destination: &toolsGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "remove a Cargo package", Destination: &toolsCargo, Local: true},
		},
		Action: runToolsUninstall,
	}
}

func toolsListCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Usage:       "List packages in the Brewfile",
		Description: "$ sci tools list\n$ sci tools list --cask",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "formula", Usage: "list formulae only", Destination: &toolsFormula, Local: true},
			&cli.BoolFlag{Name: "cask", Usage: "list casks only", Destination: &toolsCask, Local: true},
			&cli.BoolFlag{Name: "tap", Usage: "list taps only", Destination: &toolsTap, Local: true},
			&cli.BoolFlag{Name: "uv", Usage: "list uv tools only", Destination: &toolsUv, Local: true},
			&cli.BoolFlag{Name: "go", Usage: "list Go packages only", Destination: &toolsGo, Local: true},
			&cli.BoolFlag{Name: "cargo", Usage: "list Cargo packages only", Destination: &toolsCargo, Local: true},
			&cli.BoolFlag{Name: "all", Usage: "list all package types", Destination: &toolsAll, Local: true},
		},
		Action: runToolsList,
	}
}

func toolsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:        "update",
		Usage:       "Update the Homebrew registry and upgrade outdated packages",
		Description: "$ sci tools update",
		Action:      runToolsUpdate,
	}
}

func toolsOutdatedCommand() *cli.Command {
	return &cli.Command{
		Name:        "outdated",
		Usage:       "List outdated packages without upgrading",
		Description: "$ sci tools outdated",
		Action:      runToolsOutdated,
	}
}

func toolsReccsCommand() *cli.Command {
	return &cli.Command{
		Name:        "reccs",
		Usage:       "Pick optional tools to install",
		Description: "$ sci tools reccs",
		Action:      runToolsReccs,
	}
}

func resolveToolsFile() (string, error) {
	// If the user passed an explicit --file flag, honour it.
	if toolsFile != brew.DefaultBrewfile {
		return brew.ExpandPath(toolsFile)
	}
	// Otherwise search the candidate paths brew recognises.
	if found := brew.LocateBrewfile(); found != "" {
		return found, nil
	}
	return "", fmt.Errorf("no Brewfile found — run 'sci doctor' first to set up your environment")
}

// syncBrewfile reconciles the Brewfile with the system state before
// running a tools subcommand. Errors are non-fatal (printed as warnings).
// Skipped when offline to avoid hanging on brew commands.
func syncBrewfile(file string) {
	if !netutil.Online() {
		return
	}
	fmt.Fprintf(os.Stderr, "  %s Ensuring Brewfile is up-to-date…\n", ui.SymArrow)
	result, err := brew.Sync(brew.BundleRunner{}, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n",
			ui.SymWarn, ui.TUI.Warn().Render("Could not sync Brewfile: "+err.Error()))
		return
	}
	if msg := result.Human(); msg != "" {
		fmt.Fprintf(os.Stderr, "  %s %s", ui.SymArrow, msg)
	}
}

func resolveToolsPkgType() string {
	switch {
	case toolsCask:
		return "cask"
	case toolsTap:
		return "tap"
	case toolsUv:
		return "uv"
	case toolsGo:
		return "go"
	case toolsCargo:
		return "cargo"
	case toolsFormula:
		return "formula"
	default:
		return ""
	}
}

// detectPkgType auto-detects the package type by probing brew and PyPI
// concurrently. If multiple matches are found, presents an interactive
// prompt with the recommended choice pre-selected.
func detectPkgType(pkg string) (string, error) {
	var matches []brew.DetectedPackage
	if err := ui.RunWithSpinner(fmt.Sprintf("Detecting package type for %s…", pkg), func() error {
		var detectErr error
		matches, detectErr = brew.Detect(brew.LiveProber{}, pkg)
		return detectErr
	}); err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("package %q not found in Homebrew or PyPI — use --formula, --cask, or --uv to specify the type explicitly", pkg)
	case 1:
		fmt.Fprintf(os.Stderr, "  %s Detected as %s\n", ui.SymArrow, matches[0].Type)
		return matches[0].Type, nil
	default:
		return promptPkgType(pkg, matches)
	}
}

// promptPkgType presents an interactive selector when a package is found
// in multiple registries. The first match (highest priority) is pre-selected.
func promptPkgType(pkg string, matches []brew.DetectedPackage) (string, error) {
	options := make([]huh.Option[string], len(matches))
	for i, m := range matches {
		label := m.Type
		if i == 0 {
			label += " (recommended)"
		}
		options[i] = huh.NewOption(label, m.Type)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("%q was found in multiple registries", pkg)).
				Options(options...).
				Value(&selected),
		),
	).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap())

	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

func runToolsInstall(_ context.Context, cmd *cli.Command) error {
	if !netutil.Online() {
		return fmt.Errorf("no internet connection — sci tools install requires network access")
	}

	file, err := resolveToolsFile()
	if err != nil {
		return err
	}

	if !toolsDryRun {
		syncBrewfile(file)
	}

	// With a package argument: add to Brewfile and install.
	if cmd.NArg() > 0 {
		pkg := cmd.Args().First()
		pkgType := resolveToolsPkgType()

		// Auto-detect if no explicit type flag was given.
		if pkgType == "" {
			if cmdutil.IsJSON(cmd) {
				return fmt.Errorf("--json mode requires an explicit type flag (--formula, --cask, --uv, etc.)")
			}
			pkgType, err = detectPkgType(pkg)
			if err != nil {
				return err
			}
		}

		if toolsDryRun {
			ui.Hint(fmt.Sprintf("would add %s (%s) to %s", pkg, pkgType, file))
			return nil
		}

		fmt.Fprintf(os.Stderr, "  Adding %s…\n", pkg)
		result, addErr := brew.Add(brew.BundleRunner{}, file, pkg, pkgType)
		if addErr != nil {
			return addErr
		}

		syncBrewfile(file)
		cmdutil.Output(cmd, result)
		return nil
	}

	// No arguments: sync all packages from the Brewfile.
	if toolsDryRun {
		ui.Hint(fmt.Sprintf("would install all packages from %s", file))
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Installing from Brewfile…\n")
	result, instErr := brew.Install(brew.BundleRunner{}, file)
	if instErr != nil {
		return instErr
	}

	syncBrewfile(file)
	cmdutil.Output(cmd, result)
	return nil
}

func runToolsUninstall(_ context.Context, cmd *cli.Command) error {
	if !netutil.Online() {
		return fmt.Errorf("no internet connection — sci tools uninstall requires network access")
	}
	if cmd.NArg() < 1 {
		return cmdutil.UsageErrorf(cmd, "expected a package name")
	}
	file, err := resolveToolsFile()
	if err != nil {
		return err
	}

	if !toolsDryRun {
		syncBrewfile(file)
	}

	pkg := cmd.Args().First()

	if toolsDryRun {
		ui.Hint(fmt.Sprintf("would remove %s from %s", pkg, file))
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Removing %s…\n", pkg)
	result, rmErr := brew.Remove(brew.BundleRunner{}, file, pkg, resolveToolsPkgType())
	if rmErr != nil {
		return rmErr
	}

	syncBrewfile(file)
	cmdutil.Output(cmd, result)
	return nil
}

func runToolsList(_ context.Context, cmd *cli.Command) error {
	file, err := resolveToolsFile()
	if err != nil {
		return err
	}

	syncBrewfile(file)

	runner := brew.BundleRunner{}

	// Type-specific filter or --json: plain text list.
	pkgType := resolveToolsPkgType()
	if pkgType != "" || cmdutil.IsJSON(cmd) {
		if toolsAll {
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
	err = ui.RunWithSpinner("Loading package info…", func() error {
		var detErr error
		packages, detErr = brew.ListDetailed(runner, file)
		return detErr
	})
	if err != nil {
		return err
	}

	return brew.RunListTUI(packages)
}

func runToolsUpdate(_ context.Context, cmd *cli.Command) error {
	if !netutil.Online() {
		return fmt.Errorf("no internet connection — sci tools update requires network access")
	}

	runner := brew.BundleRunner{}

	if !toolsDryRun {
		if file, err := resolveToolsFile(); err == nil {
			syncBrewfile(file)
		}
	}

	if toolsDryRun {
		ui.Hint("would update the registry and upgrade outdated packages")
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Updating package registry…\n")
	result, err := brew.Update(runner, false)
	if err != nil {
		return err
	}

	if file, err := resolveToolsFile(); err == nil {
		syncBrewfile(file)
	}
	cmdutil.Output(cmd, result)
	return nil
}

func runToolsOutdated(_ context.Context, cmd *cli.Command) error {
	if !netutil.Online() {
		return fmt.Errorf("no internet connection — sci tools outdated requires network access")
	}

	runner := brew.BundleRunner{}

	if !toolsDryRun {
		if file, err := resolveToolsFile(); err == nil {
			syncBrewfile(file)
		}
	}

	if toolsDryRun {
		ui.Hint("would update the registry and list outdated packages")
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Checking for outdated packages…\n")
	result, err := brew.Update(runner, true)
	if err != nil {
		return err
	}

	if file, err := resolveToolsFile(); err == nil {
		syncBrewfile(file)
	}
	cmdutil.Output(cmd, result)
	return nil
}

func runToolsReccs(_ context.Context, cmd *cli.Command) error {
	runner := brew.BundleRunner{}

	if file, err := resolveToolsFile(); err == nil {
		syncBrewfile(file)
	}

	if cmdutil.IsJSON(cmd) {
		result, err := doctor.ListOptionalTools(runner)
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	result, err := doctor.RunOptionalSetup(runner)
	if err != nil {
		return err
	}

	if len(result.Installed) > 0 {
		if file, err := resolveToolsFile(); err == nil {
			syncBrewfile(file)
		}
	}

	cmdutil.Output(cmd, result)
	return nil
}
