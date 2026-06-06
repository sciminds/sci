//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/doctor"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/uikit"
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
		Description: "$ sci tools install\n$ sci tools install pandoc\n$ sci tools uninstall pandoc\n$ sci tools list\n$ sci tools update\n$ sci tools outdated\n$ sci tools reccs\n$ sci tools apps",
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
		Action: func(_ context.Context, cmd *cli.Command) error {
			if file, err := resolveToolsFile(); err == nil {
				syncBrewfile(file)
			}
			return cli.ShowSubcommandHelp(cmd)
		},
		Commands: []*cli.Command{
			toolsInstallCommand(),
			toolsUninstallCommand(),
			toolsListCommand(),
			toolsUpdateCommand(),
			toolsOutdatedCommand(),
			toolsReccsCommand(),
			toolsAppsCommand(),
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
			&cli.BoolFlag{Name: "formula", Usage: "add as a brew formula", Destination: &toolsFormula, Local: true},
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

// reccsOpts holds the flags shared by `tools reccs` and its `tools apps` alias.
type reccsOpts struct {
	installName string
	all         bool
	includeCSV  string
	excludeCSV  string
	dryRun      bool
}

// reccsFlags builds the flag set shared by reccs and apps, binding into o.
func reccsFlags(o *reccsOpts) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "install", Usage: "install a named tool without the picker", Destination: &o.installName, Local: true},
		&cli.BoolFlag{Name: "all", Usage: "install every missing recommendation", Destination: &o.all, Local: true},
		&cli.StringFlag{Name: "include", Usage: "comma-separated tools to install (skips already-installed)", Destination: &o.includeCSV, Local: true},
		&cli.StringFlag{Name: "exclude", Usage: "comma-separated tools to skip; install the rest", Destination: &o.excludeCSV, Local: true},
		&cli.BoolFlag{Name: "dry-run", Usage: "preview the resolved set without installing", Destination: &o.dryRun, Local: true},
	}
}

func toolsReccsCommand() *cli.Command {
	var o reccsOpts
	var apps bool
	flags := append(reccsFlags(&o),
		&cli.BoolFlag{Name: "apps", Usage: "limit to recommended GUI apps (casks)", Destination: &apps, Local: true},
	)
	return &cli.Command{
		Name:  "reccs",
		Usage: "Pick recommended tools to install",
		Description: "$ sci tools reccs                              # interactive multi-select picker\n" +
			"$ sci tools reccs --apps                       # just the GUI apps\n" +
			"$ sci tools reccs --install pandoc             # single, non-interactive\n" +
			"$ sci tools reccs --all                        # install everything missing\n" +
			"$ sci tools reccs --include bat,fd             # install just these\n" +
			"$ sci tools reccs --exclude quarto             # skip these, install the rest\n" +
			"$ sci tools reccs --apps --all --dry-run       # preview all missing apps",
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runToolsReccs(ctx, cmd, o, apps)
		},
	}
}

// toolsAppsCommand is a discoverable alias for `tools reccs --apps`: the
// recommendations surface pre-scoped to GUI apps (casks).
func toolsAppsCommand() *cli.Command {
	var o reccsOpts
	return &cli.Command{
		Name:  "apps",
		Usage: "Pick recommended GUI apps (casks) to install",
		Description: "$ sci tools apps                               # interactive multi-select picker\n" +
			"$ sci tools apps --all                         # install every missing app\n" +
			"$ sci tools apps --include obsidian,raycast    # install just these\n" +
			"$ sci tools apps --all --dry-run               # preview without installing",
		Flags: reccsFlags(&o),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runToolsReccs(ctx, cmd, o, true)
		},
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
	fmt.Fprintf(os.Stderr, "  %s Ensuring Brewfile is up-to-date…\n", uikit.SymArrow)
	result, err := brew.Sync(brew.CLI{}, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n",
			uikit.SymWarn, uikit.TUI.Warn().Render("Could not sync Brewfile: "+err.Error()))
		return
	}
	if msg := result.Human(); msg != "" {
		fmt.Fprintf(os.Stderr, "  %s %s", uikit.SymArrow, msg)
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
	if err := uikit.RunWithSpinner(fmt.Sprintf("Detecting package type for %s…", pkg), func() error {
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
		fmt.Fprintf(os.Stderr, "  %s Detected as %s\n", uikit.SymArrow, matches[0].Type)
		return matches[0].Type, nil
	default:
		return promptPkgType(pkg, matches)
	}
}

// promptPkgType presents an interactive selector when a package is found
// in multiple registries. The first match (highest priority) is pre-selected.
func promptPkgType(pkg string, matches []brew.DetectedPackage) (string, error) {
	options := make([]uikit.Option[string], len(matches))
	for i, m := range matches {
		label := m.Type
		if i == 0 {
			label += " (recommended)"
		}
		options[i] = uikit.NewOption(label, m.Type)
	}

	return uikit.Select(fmt.Sprintf("%q was found in multiple registries", pkg), options)
}

func runToolsInstall(_ context.Context, cmd *cli.Command) error {
	if !netutil.Online() {
		return fmt.Errorf("no internet connection — sci tools install requires network access")
	}

	file, err := resolveToolsFile()
	if err != nil {
		return err
	}

	runner := brew.CLI{}

	// With a package argument: update registry, install directly, sync Brewfile.
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
			uikit.Hint(fmt.Sprintf("would add %s (%s) to %s", pkg, pkgType, file))
			return nil
		}

		fmt.Fprintf(os.Stderr, "  Updating package registry…\n")
		if err := runner.Update(); err != nil {
			return fmt.Errorf("brew update: %w", err)
		}

		fmt.Fprintf(os.Stderr, "  Installing %s…\n", pkg)
		result, addErr := brew.Add(runner, file, pkg, pkgType)
		if addErr != nil {
			return addErr
		}

		cmdutil.Output(cmd, result)
		return nil
	}

	// No arguments: install all declared packages from the Brewfile.
	// Intentionally no pre-Install Sync: Sync strips Brewfile entries that
	// aren't installed yet, which is exactly the set Install is about to
	// install. The Brewfile is user intent; we don't overwrite intent with
	// current system state before acting on it.
	if toolsDryRun {
		uikit.Hint(fmt.Sprintf("would install all packages from %s", file))
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Installing from Brewfile…\n")
	result, instErr := brew.Install(runner, file)
	if instErr != nil {
		return instErr
	}

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

	pkg := cmd.Args().First()

	if toolsDryRun {
		uikit.Hint(fmt.Sprintf("would remove %s from %s", pkg, file))
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Removing %s…\n", pkg)
	result, rmErr := brew.Remove(brew.CLI{}, file, pkg, resolveToolsPkgType())
	if rmErr != nil {
		return rmErr
	}

	cmdutil.Output(cmd, result)
	return nil
}

func runToolsList(_ context.Context, cmd *cli.Command) error {
	file, err := resolveToolsFile()
	if err != nil {
		return err
	}

	syncBrewfile(file)

	runner := brew.CLI{}

	// Type-specific filter or --json: plain text list.
	pkgType := resolveToolsPkgType()
	if pkgType != "" || cmdutil.IsJSON(cmd) {
		if toolsAll {
			pkgType = ""
		}
		result, listErr := brew.List(file, pkgType)
		if listErr != nil {
			return listErr
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	// Default: interactive TUI with descriptions.
	var packages []brew.PackageInfo
	err = uikit.RunWithSpinner("Loading package info…", func() error {
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

	if toolsDryRun {
		uikit.Hint("would update the registry and upgrade outdated packages")
		return nil
	}

	runner := brew.CLI{}

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

	if toolsDryRun {
		uikit.Hint("would update the registry and list outdated packages")
		return nil
	}

	runner := brew.CLI{}

	fmt.Fprintf(os.Stderr, "  Checking for outdated packages…\n")
	result, err := brew.Update(runner, true)
	if err != nil {
		return err
	}

	// In JSON mode or no outdated packages, just output and return.
	if cmdutil.IsJSON(cmd) || len(result.Outdated) == 0 {
		cmdutil.Output(cmd, result)
		return nil
	}

	// Show the outdated list and offer to upgrade.
	fmt.Fprintf(os.Stderr, "\n  %d outdated package(s):\n", len(result.Outdated))
	for _, pkg := range result.Outdated {
		arrow := uikit.TUI.TextPink().Render(" → ")
		version := uikit.TUI.TextPink().Render(pkg.InstalledVersion) + arrow + pkg.CurrentVersion
		fmt.Fprintf(os.Stderr, "    %s %s\n", pkg.Name, version)
	}
	fmt.Fprintln(os.Stderr)

	upgradeErr := cmdutil.ConfirmYes("Upgrade outdated packages?")
	if errors.Is(upgradeErr, cmdutil.ErrCancelled) {
		fmt.Fprintf(os.Stderr, "\n  To upgrade later:\n")
		fmt.Fprintf(os.Stderr, "    %s sci tools update\n", uikit.SymArrow)
		fmt.Fprintln(os.Stderr)
		return nil
	}
	if upgradeErr != nil {
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Upgrading…\n")
	upgradeResult, err := brew.UpgradeOnly(runner)
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, upgradeResult)
	return nil
}

func runToolsReccs(_ context.Context, cmd *cli.Command, o reccsOpts, apps bool) error {
	runner := brew.CLI{}

	include := splitCSV(o.includeCSV)
	exclude := splitCSV(o.excludeCSV)

	// Mutex: at most one of --install / --all / --include / --exclude.
	bulkModes := lo.Count([]bool{o.installName != "", o.all, len(include) > 0, len(exclude) > 0}, true)
	if bulkModes > 1 {
		return errors.New("--install, --all, --include, and --exclude are mutually exclusive")
	}

	// JSON catalog-listing mode: no bulk flag, no install, no dry-run.
	if cmdutil.IsJSON(cmd) && bulkModes == 0 && !o.dryRun {
		result, err := doctor.ListOptionalTools(runner, apps)
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	// Resolve Brewfile path so install functions can sync it afterward.
	brewfilePath, _ := resolveToolsFile()

	// Bulk paths: --all / --include / --exclude (and dry-run, which implies bulk).
	if o.all || len(include) > 0 || len(exclude) > 0 || o.dryRun {
		filter := doctor.OptionalFilter{All: o.all, Include: include, Exclude: exclude, Apps: apps}
		if !o.all && len(include) == 0 && len(exclude) == 0 {
			// Bare --dry-run with no scope → preview "all missing".
			filter.All = true
		}
		entries, err := doctor.ResolveOptionalSet(runner, filter)
		if err != nil {
			return err
		}
		result, err := doctor.InstallOptionalTools(runner, entries, brewfilePath, o.dryRun)
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		if len(result.Failed) > 0 {
			return fmt.Errorf("%d optional tool(s) failed to install", len(result.Failed))
		}
		return nil
	}

	// Single-tool and multi-select picker paths.
	var result doctor.OptionalSetupResult
	var err error
	if o.installName != "" {
		result, err = doctor.InstallOptionalTool(runner, o.installName, brewfilePath, apps)
	} else {
		result, err = doctor.RunOptionalSetup(runner, brewfilePath, apps)
	}
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)
	return nil
}

// splitCSV trims whitespace and drops empty entries from a comma-separated list.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
