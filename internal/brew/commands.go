package brew

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/samber/lo"
)

// Add installs a package directly and syncs the Brewfile to pick it up.
func Add(r Runner, file, pkg, pkgType string) (AddResult, error) {
	if err := r.DirectInstall(pkg, pkgType); err != nil {
		return AddResult{}, fmt.Errorf("install %s: %w", pkg, err)
	}

	if _, err := Sync(r, file); err != nil {
		return AddResult{}, fmt.Errorf("sync brewfile: %w", err)
	}

	return AddResult{Package: pkg, Type: pkgType}, nil
}

// Remove uninstalls a package directly and syncs the Brewfile to drop it.
func Remove(r Runner, file, pkg, pkgType string) (RemoveResult, error) {
	if err := r.DirectUninstall(pkg, pkgType); err != nil {
		return RemoveResult{}, fmt.Errorf("uninstall %s: %w", pkg, err)
	}

	if _, err := Sync(r, file); err != nil {
		return RemoveResult{}, fmt.Errorf("sync brewfile: %w", err)
	}

	return RemoveResult{Package: pkg, Type: pkgType}, nil
}

// Install installs missing packages from the Brewfile. It collects a system
// snapshot, diffs against the Brewfile entries, and batch-installs each type
// via [InstallEntries].
func Install(r Runner, file string) (InstallResult, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return InstallResult{}, fmt.Errorf("read Brewfile: %w", err)
	}

	snap, err := CollectSnapshot(r)
	if err != nil {
		return InstallResult{}, fmt.Errorf("collect snapshot: %w", err)
	}

	entries := ParseBrewfileEntries(string(content))
	missing := lo.Filter(entries, func(e BrewfileEntry, _ int) bool {
		return !snap.IsInstalled(e.Type, e.Name)
	})

	if len(missing) == 0 {
		return InstallResult{}, nil
	}

	installed, err := InstallEntries(r, missing)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Installed: installed}, nil
}

// InstallEntries installs the given Brewfile entries in dependency order:
// taps → formulae → casks → uv tools. Returns the names of installed packages.
func InstallEntries(r Runner, entries []BrewfileEntry) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	groups := lo.GroupBy(entries, func(e BrewfileEntry) string { return e.Type })
	names := func(typ string) []string {
		return lo.Map(groups[typ], func(e BrewfileEntry, _ int) string { return e.Name })
	}

	// Taps first (individually — needed before tap-qualified formulae).
	for _, name := range names("tap") {
		if err := r.DirectInstall(name, "tap"); err != nil {
			return nil, fmt.Errorf("tap %s: %w", name, err)
		}
	}
	if err := r.InstallFormulae(names("brew")); err != nil {
		return nil, fmt.Errorf("install formulae: %w", err)
	}
	if err := r.InstallCasks(names("cask")); err != nil {
		return nil, fmt.Errorf("install casks: %w", err)
	}
	if err := r.InstallUVTools(names("uv")); err != nil {
		return nil, fmt.Errorf("install uv tools: %w", err)
	}

	installed := lo.Map(entries, func(e BrewfileEntry, _ int) string { return e.Name })
	return installed, nil
}

// List lists packages from the Brewfile, optionally filtered by type.
// Parses the Brewfile directly — no subprocess needed.
func List(file, pkgType string) (ListResult, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return ListResult{}, fmt.Errorf("read Brewfile: %w", err)
	}

	entries := ParseBrewfileEntries(string(content))
	if pkgType != "" {
		entries = lo.Filter(entries, func(e BrewfileEntry, _ int) bool {
			// Callers pass "formula" but Brewfile uses "brew".
			return e.Type == pkgType || (pkgType == "formula" && e.Type == "brew")
		})
	}

	names := lo.Map(entries, func(e BrewfileEntry, _ int) string { return e.Name })
	return ListResult{Packages: names, Type: pkgType}, nil
}

// Update refreshes the Homebrew registry and optionally upgrades outdated packages.
// If checkOnly is true, it only lists outdated packages without upgrading.
func Update(r Runner, checkOnly bool) (UpdateResult, error) {
	if err := r.Update(); err != nil {
		return UpdateResult{}, fmt.Errorf("brew update: %w", err)
	}

	// Check brew and uv outdated concurrently.
	brewOutdated, uvOutdated, err := checkOutdated(r)
	if err != nil {
		return UpdateResult{}, err
	}

	outdated := append(brewOutdated, uvOutdated...)

	if checkOnly || len(outdated) == 0 {
		return UpdateResult{Outdated: outdated, CheckOnly: checkOnly}, nil
	}

	upgradeOut, err := runUpgrades(r, brewOutdated, uvOutdated)
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{Outdated: outdated, CheckOnly: false, UpgradeOutput: upgradeOut}, nil
}

// UpgradeOnly upgrades outdated packages without refreshing the registry.
// Use when the registry was already refreshed by an earlier Update call.
func UpgradeOnly(r Runner) (UpdateResult, error) {
	// Re-check what's outdated (fast — registry is already fresh).
	brewOutdated, uvOutdated, err := checkOutdated(r)
	if err != nil {
		return UpdateResult{}, err
	}

	allOutdated := append(brewOutdated, uvOutdated...)
	if len(allOutdated) == 0 {
		return UpdateResult{}, nil
	}

	upgradeOut, err := runUpgrades(r, brewOutdated, uvOutdated)
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{Outdated: allOutdated, UpgradeOutput: upgradeOut}, nil
}

// checkOutdated queries brew and uv outdated concurrently.
func checkOutdated(r Runner) (brewOutdated, uvOutdated []OutdatedPackage, err error) {
	var (
		brewErr, uvErr error
		wg             sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		brewOutdated, brewErr = r.Outdated()
	}()
	go func() {
		defer wg.Done()
		uvOutdated, uvErr = r.UVOutdated()
	}()
	wg.Wait()

	if brewErr != nil {
		return nil, nil, fmt.Errorf("brew outdated: %w", brewErr)
	}
	if uvErr != nil {
		return nil, nil, fmt.Errorf("uv outdated: %w", uvErr)
	}
	return brewOutdated, uvOutdated, nil
}

// runUpgrades runs brew upgrade and uv upgrade for the given outdated packages.
func runUpgrades(r Runner, brewOutdated, uvOutdated []OutdatedPackage) (string, error) {
	var upgradeOut string
	if len(brewOutdated) > 0 {
		out, err := r.Upgrade()
		if err != nil {
			return "", fmt.Errorf("brew upgrade: %w", err)
		}
		upgradeOut = out
	}
	if len(uvOutdated) > 0 {
		names := lo.Map(uvOutdated, func(pkg OutdatedPackage, _ int) string {
			return pkg.Name
		})
		out, err := r.UVUpgrade(names)
		if err != nil {
			return upgradeOut, fmt.Errorf("uv upgrade: %w", err)
		}
		upgradeOut += out
	}
	return upgradeOut, nil
}

// ListDetailed parses the Brewfile for formula/cask names, fetches descriptions
// via brew info in parallel, and returns a sorted combined slice.
func ListDetailed(r Runner, file string) ([]PackageInfo, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

	entries := ParseBrewfileEntries(string(content))
	formulae := lo.FilterMap(entries, func(e BrewfileEntry, _ int) (string, bool) {
		return e.Name, e.Type == "brew"
	})
	casks := lo.FilterMap(entries, func(e BrewfileEntry, _ int) (string, bool) {
		return e.Name, e.Type == "cask"
	})

	// Fetch descriptions in parallel.
	var (
		formulaeInfo, casksInfo []PackageInfo
		fInfoErr, cInfoErr      error
		wg                      sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		formulaeInfo, fInfoErr = r.Info(formulae, false)
	}()
	go func() {
		defer wg.Done()
		casksInfo, cInfoErr = r.Info(casks, true)
	}()
	wg.Wait()

	if fInfoErr != nil {
		return nil, fmt.Errorf("info formulae: %w", fInfoErr)
	}
	if cInfoErr != nil {
		return nil, fmt.Errorf("info casks: %w", cInfoErr)
	}

	all := append(formulaeInfo, casksInfo...)
	slices.SortFunc(all, func(a, b PackageInfo) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return all, nil
}
