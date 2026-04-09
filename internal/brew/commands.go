package brew

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

// Add adds a package to the Brewfile and installs it.
// If install fails, the Brewfile is restored to its previous state.
func Add(r Runner, file, pkg, pkgType string) (AddResult, error) {
	backup, err := os.ReadFile(file)
	if err != nil {
		return AddResult{}, fmt.Errorf("read brewfile: %w", err)
	}

	if err := r.BundleAdd(file, pkg, pkgType); err != nil {
		return AddResult{}, fmt.Errorf("bundle add: %w", err)
	}

	if _, err := r.BundleInstall(file); err != nil {
		// Rollback: restore the Brewfile.
		_ = os.WriteFile(file, backup, 0o644)
		return AddResult{}, fmt.Errorf("bundle install (rolled back): %w", err)
	}

	return AddResult{Package: pkg, Type: pkgType}, nil
}

// Remove removes a package from the Brewfile and cleans up.
// If cleanup fails, the Brewfile is restored to its previous state.
func Remove(r Runner, file, pkg, pkgType string) (RemoveResult, error) {
	backup, err := os.ReadFile(file)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("read brewfile: %w", err)
	}

	if err := r.BundleRemove(file, pkg, pkgType); err != nil {
		return RemoveResult{}, fmt.Errorf("bundle remove: %w", err)
	}

	if _, err := r.BundleCleanup(file); err != nil {
		// Rollback: restore the Brewfile.
		_ = os.WriteFile(file, backup, 0o644)
		return RemoveResult{}, fmt.Errorf("bundle cleanup (rolled back): %w", err)
	}

	return RemoveResult{Package: pkg, Type: pkgType}, nil
}

// Install installs all packages from the Brewfile.
func Install(r Runner, file string) (InstallResult, error) {
	out, err := r.BundleInstall(file)
	if err != nil {
		return InstallResult{}, fmt.Errorf("bundle install: %w", err)
	}
	return InstallResult{Output: out}, nil
}

// List lists packages from the Brewfile, optionally filtered by type.
func List(r Runner, file, pkgType string) (ListResult, error) {
	pkgs, err := r.BundleList(file, pkgType)
	if err != nil {
		return ListResult{}, fmt.Errorf("bundle list: %w", err)
	}
	return ListResult{Packages: pkgs, Type: pkgType}, nil
}

// Update refreshes the Homebrew registry and optionally upgrades outdated packages.
// If checkOnly is true, it only lists outdated packages without upgrading.
// setTitle and setStatus update the spinner's main label and detail text respectively.
func Update(r Runner, checkOnly bool, setTitle, setStatus func(string)) (UpdateResult, error) {
	onLine := func(s string) {
		if setStatus != nil {
			setStatus(s)
		}
	}

	if err := r.Update(onLine); err != nil {
		return UpdateResult{}, fmt.Errorf("brew update: %w", err)
	}

	if setTitle != nil {
		setTitle("Checking for outdated packages…")
	}

	// Check brew and uv outdated concurrently.
	var (
		brewOutdated, uvOutdated []OutdatedPackage
		brewErr, uvErr           error
		wg                       sync.WaitGroup
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
		return UpdateResult{}, fmt.Errorf("brew outdated: %w", brewErr)
	}
	if uvErr != nil {
		return UpdateResult{}, fmt.Errorf("uv outdated: %w", uvErr)
	}

	outdated := append(brewOutdated, uvOutdated...)

	if checkOnly || len(outdated) == 0 {
		return UpdateResult{Outdated: outdated, CheckOnly: checkOnly}, nil
	}

	if setTitle != nil {
		setTitle(fmt.Sprintf("Upgrading %d package(s)…", len(outdated)))
	}

	var upgradeOut string
	if len(brewOutdated) > 0 {
		out, err := r.Upgrade(onLine)
		if err != nil {
			return UpdateResult{}, fmt.Errorf("brew upgrade: %w", err)
		}
		upgradeOut = out
	}
	if len(uvOutdated) > 0 {
		out, err := r.UVUpgrade(onLine)
		if err != nil {
			return UpdateResult{}, fmt.Errorf("uv upgrade: %w", err)
		}
		upgradeOut += out
	}

	return UpdateResult{Outdated: outdated, CheckOnly: false, UpgradeOutput: upgradeOut}, nil
}

// ListDetailed fetches formulae and casks with descriptions in parallel.
// Returns a combined slice of PackageInfo sorted by type (formulae first, then casks).
func ListDetailed(r Runner, file string) ([]PackageInfo, error) {
	var (
		formulae, casks []string
		wg              sync.WaitGroup
		formulaeErr     error
		casksErr        error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		formulae, formulaeErr = r.BundleList(file, "formula")
	}()
	go func() {
		defer wg.Done()
		casks, casksErr = r.BundleList(file, "cask")
	}()
	wg.Wait()

	if formulaeErr != nil {
		return nil, fmt.Errorf("list formulae: %w", formulaeErr)
	}
	if casksErr != nil {
		return nil, fmt.Errorf("list casks: %w", casksErr)
	}

	// Fetch descriptions in parallel.
	var (
		formulaeInfo, casksInfo []PackageInfo
		fInfoErr, cInfoErr      error
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
	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})
	return all, nil
}
