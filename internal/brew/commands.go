package brew

import (
	"cmp"
	"errors"
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
//
// Sync always runs at the end — even when InstallEntries reports errors —
// so the Brewfile reflects what's actually installed. Without this, a
// partial failure (e.g. one cask conflicts with a manually-installed app)
// would leave the user's Brewfile out of sync with reality and the failure
// would repeat on every doctor run.
func Install(r Runner, file string) (InstallResult, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return InstallResult{}, fmt.Errorf("read Brewfile: %w", err)
	}

	// ForBrewfile so externally-installed casks (e.g. Zoom from the vendor's
	// .pkg) aren't tried again on every doctor run.
	snap, err := CollectSnapshotForBrewfile(r, string(content))
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

	installed, installErr := InstallEntries(r, missing)
	result := InstallResult{Installed: installed}

	// Reconcile the Brewfile with the new system state regardless of
	// install errors so successful installs land in the Brewfile and
	// failed ones don't linger as unmet intent on the next run.
	_, syncErr := Sync(r, file)
	if syncErr != nil {
		syncErr = fmt.Errorf("sync brewfile: %w", syncErr)
	}

	return result, errors.Join(installErr, syncErr)
}

// InstallEntries installs the given Brewfile entries in dependency order:
// taps → formulae → casks → uv tools. Returns the names of packages that
// were actually requested (the install attempt — successful or not) and
// a joined error covering every phase that failed.
//
// All four phases run regardless of intermediate failures: one bad cask
// shouldn't keep `gh`, `uv`, or `marimo` from installing. Callers should
// check `err == nil` for full success; on partial success they get the
// joined error plus the list of names that were dispatched.
func InstallEntries(r Runner, entries []BrewfileEntry) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	groups := lo.GroupBy(entries, func(e BrewfileEntry) string { return e.Type })
	names := func(typ string) []string {
		return lo.Map(groups[typ], func(e BrewfileEntry, _ int) string { return e.Name })
	}
	// Spec falls back to Name for entries built outside the parser (tests,
	// hand-constructed entries) where the bracket-preserving Spec wasn't set.
	specs := func(typ string) []string {
		return lo.Map(groups[typ], func(e BrewfileEntry, _ int) string {
			return cmp.Or(e.Spec, e.Name)
		})
	}

	var errs []error

	// Taps first (individually — needed before tap-qualified formulae).
	for _, name := range names("tap") {
		if err := r.DirectInstall(name, "tap"); err != nil {
			errs = append(errs, fmt.Errorf("tap %s: %w", name, err))
		}
	}
	if err := r.InstallFormulae(names("brew")); err != nil {
		errs = append(errs, fmt.Errorf("install formulae: %w", err))
	}
	if err := r.InstallCasks(names("cask")); err != nil {
		errs = append(errs, fmt.Errorf("install casks: %w", err))
	}
	// uv tools install via spec so bracket extras like "[standard]" reach uv.
	if err := r.InstallUVTools(specs("uv")); err != nil {
		errs = append(errs, fmt.Errorf("install uv tools: %w", err))
	}

	installed := lo.Map(entries, func(e BrewfileEntry, _ int) string { return e.Name })
	return installed, errors.Join(errs...)
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

	check, err := checkOutdated(r)
	if err != nil {
		return UpdateResult{}, err
	}
	if checkOnly {
		return UpdateResult{Outdated: check.All(), CheckOnly: true}, nil
	}
	return upgradeChecked(r, check)
}

// UpgradeOnly upgrades outdated packages without refreshing the registry.
// Use when the registry was already refreshed by an earlier Update call.
func UpgradeOnly(r Runner) (UpdateResult, error) {
	// Re-check what's outdated (fast — registry is already fresh).
	check, err := checkOutdated(r)
	if err != nil {
		return UpdateResult{}, err
	}
	return upgradeChecked(r, check)
}

// upgradeChecked upgrades the packages check found upgradable.
func upgradeChecked(r Runner, check OutdatedCheck) (UpdateResult, error) {
	upgradeOut, err := runUpgrades(r, check.Brew, check.UV)
	if err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Outdated: check.All(), UpgradeOutput: upgradeOut}, nil
}

// checkOutdated queries brew and uv outdated concurrently and sets pinned
// packages apart from the upgradable ones.
func checkOutdated(r Runner) (OutdatedCheck, error) {
	var (
		brewOutdated, uvOutdated []OutdatedPackage
		brewErr, uvErr           error
		wg                       sync.WaitGroup
	)
	wg.Go(func() { brewOutdated, brewErr = r.Outdated() })
	wg.Go(func() { uvOutdated, uvErr = r.UVOutdated() })
	wg.Wait()

	if brewErr != nil {
		return OutdatedCheck{}, fmt.Errorf("brew outdated: %w", brewErr)
	}
	if uvErr != nil {
		return OutdatedCheck{}, fmt.Errorf("uv outdated: %w", uvErr)
	}

	pinned, upgradable := lo.FilterReject(brewOutdated, func(p OutdatedPackage, _ int) bool {
		return p.Pinned
	})
	return OutdatedCheck{Brew: upgradable, UV: uvOutdated, Pinned: pinned}, nil
}

// UpgradeBrew upgrades exactly the given Homebrew packages, formulae and casks
// in separate calls. A failure in one does not stop the other.
func UpgradeBrew(r Runner, pkgs []OutdatedPackage) (string, error) {
	namesOfType := func(pkgType string) []string {
		return lo.FilterMap(pkgs, func(p OutdatedPackage, _ int) (string, bool) {
			return p.Name, p.Type == pkgType
		})
	}
	var (
		upgradeOut string
		errs       []error
	)
	if formulae := namesOfType(TypeFormula); len(formulae) > 0 {
		out, err := r.UpgradeFormulae(formulae)
		upgradeOut += out
		if err != nil {
			errs = append(errs, fmt.Errorf("brew upgrade --formula: %w", err))
		}
	}
	if casks := namesOfType(TypeCask); len(casks) > 0 {
		out, err := r.UpgradeCasks(casks)
		upgradeOut += out
		if err != nil {
			errs = append(errs, fmt.Errorf("brew upgrade --cask: %w", err))
		}
	}
	return upgradeOut, errors.Join(errs...)
}

// runUpgrades runs brew upgrade and uv upgrade for the given outdated packages.
// Both phases run regardless of intermediate failures: a brew upgrade error
// (e.g. one cask prompts for a password and times out) shouldn't strand uv
// tools as still-outdated when uv has no shared failure mode.
func runUpgrades(r Runner, brewOutdated, uvOutdated []OutdatedPackage) (string, error) {
	upgradeOut, brewErr := UpgradeBrew(r, brewOutdated)
	errs := []error{brewErr}
	if len(uvOutdated) > 0 {
		names := lo.Map(uvOutdated, func(pkg OutdatedPackage, _ int) string {
			return pkg.Name
		})
		out, err := r.UVUpgrade(ResolveUVSpecs(names))
		upgradeOut += out
		if err != nil {
			errs = append(errs, fmt.Errorf("uv upgrade: %w", err))
		}
	}
	return upgradeOut, errors.Join(errs...)
}

// ResolveUVSpecs maps uv package names to their Brewfile install specs so
// bracket extras (e.g. `marimo[recommended]`) survive the upgrade reinstall.
// Names without a matching `uv "..."` entry — or when no Brewfile is found —
// pass through unchanged.
func ResolveUVSpecs(names []string) []string {
	specs := loadUVSpecMap()
	return lo.Map(names, func(name string, _ int) string {
		if spec, ok := specs[name]; ok {
			return spec
		}
		return name
	})
}

func loadUVSpecMap() map[string]string {
	path := LocateBrewfile()
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	specs := map[string]string{}
	for _, e := range ParseBrewfileEntries(string(content)) {
		if e.Type != "uv" {
			continue
		}
		specs[e.Name] = cmp.Or(e.Spec, e.Name)
	}
	return specs
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
