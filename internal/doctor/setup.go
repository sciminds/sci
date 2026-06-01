package doctor

// setup.go — optional tool picker via interactive list TUI.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
)

// OptionalSetupResult reports the outcome of an optional-tool install.
//
// Single-install and TUI paths populate Installed with the one name and leave
// the bulk fields empty. Bulk paths (--all / --include / --exclude) populate
// Skipped (already-installed and filtered out), Failed (per-tool errors), and
// DryRun (true if no actions ran). Installed always lists what made it onto
// disk; in dry-run mode it lists what *would* have been installed.
type OptionalSetupResult struct {
	Installed []string        `json:"installed"`
	Skipped   []string        `json:"skipped,omitempty"`
	Failed    []FailedInstall `json:"failed,omitempty"`
	DryRun    bool            `json:"dry_run,omitempty"`
	Output    string          `json:"output,omitempty"`
}

// FailedInstall reports a single tool whose install errored. Used in the
// continue-on-error bulk path so callers can see every failure at once.
type FailedInstall struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// JSON returns the structured output.
func (r OptionalSetupResult) JSON() any { return r }

// Human returns the styled terminal output.
func (r OptionalSetupResult) Human() string {
	// Bulk path: any of skipped/failed/dry-run set, or multi-install.
	bulk := r.DryRun || len(r.Skipped) > 0 || len(r.Failed) > 0 || len(r.Installed) > 1
	if !bulk {
		if len(r.Installed) == 0 {
			return "No tools selected.\n"
		}
		return fmt.Sprintf("Installed %d tools: %s\n", len(r.Installed), strings.Join(r.Installed, ", "))
	}

	var b strings.Builder
	if r.DryRun {
		fmt.Fprintf(&b, "Dry run — would install %d tools:\n", len(r.Installed))
		for _, n := range r.Installed {
			fmt.Fprintf(&b, "  + %s\n", n)
		}
	} else if len(r.Installed) > 0 {
		fmt.Fprintf(&b, "Installed %d tools: %s\n", len(r.Installed), strings.Join(r.Installed, ", "))
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "Skipped %d (already installed): %s\n", len(r.Skipped), strings.Join(r.Skipped, ", "))
	}
	if len(r.Failed) > 0 {
		fmt.Fprintf(&b, "Failed %d:\n", len(r.Failed))
		for _, f := range r.Failed {
			fmt.Fprintf(&b, "  - %s: %s\n", f.Name, f.Error)
		}
	}
	if b.Len() == 0 {
		return "No tools selected.\n"
	}
	return b.String()
}

// OptionalToolInfo describes an optional tool and its install status.
type OptionalToolInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Installed bool   `json:"installed"`
}

// OptionalToolsResult reports optional tools and their install status.
type OptionalToolsResult struct {
	Tools []OptionalToolInfo `json:"tools"`
}

// JSON returns the structured output.
func (r OptionalToolsResult) JSON() any { return r }

// Human returns the styled terminal output.
func (r OptionalToolsResult) Human() string {
	if len(r.Tools) == 0 {
		return "No optional tools available.\n"
	}
	var b strings.Builder
	for _, t := range r.Tools {
		status := lo.Ternary(t.Installed, "installed", "missing")
		fmt.Fprintf(&b, "  %s (%s): %s\n", t.Name, t.Type, status)
	}
	return b.String()
}

// optionalCatalog returns the optional-tool catalog parsed from
// BrewfileOptional, optionally narrowed to GUI apps (casks). "Apps" is not a
// separate list — it's the cask subset of the optional catalog — so bare
// reccs and the --apps view share a single source of truth.
func optionalCatalog(apps bool) []brew.BrewfileEntry {
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	if !apps {
		return entries
	}
	return lo.Filter(entries, func(e brew.BrewfileEntry, _ int) bool {
		return e.Type == "cask"
	})
}

// ListOptionalTools returns optional tools with their install status,
// without prompting the user. Used in --json mode. When apps is true the
// catalog is narrowed to GUI apps (casks).
func ListOptionalTools(r brew.Runner, apps bool) (OptionalToolsResult, error) {
	entries := optionalCatalog(apps)
	if len(entries) == 0 {
		return OptionalToolsResult{}, nil
	}

	missing := missingSet(r, BrewfileOptional)

	tools := lo.Map(entries, func(e brew.BrewfileEntry, _ int) OptionalToolInfo {
		return OptionalToolInfo{
			Name:      e.Name,
			Type:      e.Type,
			Installed: !missing[e.Name],
		}
	})
	return OptionalToolsResult{Tools: tools}, nil
}

// InstallOptionalTool installs a named optional tool without interactive
// prompts. Returns an error if the tool is not in the optional list or is
// already installed. When brewfilePath is non-empty, the Brewfile is synced
// after install so the new package appears immediately. When apps is true the
// lookup is scoped to GUI apps (casks), so naming a non-cask errors.
func InstallOptionalTool(r brew.Runner, name, brewfilePath string, apps bool) (OptionalSetupResult, error) {
	entries := optionalCatalog(apps)
	entry, ok := lo.Find(entries, func(e brew.BrewfileEntry) bool {
		return e.Name == name
	})
	if !ok {
		available := lo.Map(entries, func(e brew.BrewfileEntry, _ int) string { return e.Name })
		return OptionalSetupResult{}, fmt.Errorf("unknown optional tool %q (available: %s)", name, strings.Join(available, ", "))
	}

	missing := missingSet(r, BrewfileOptional)
	if !missing[name] {
		return OptionalSetupResult{}, fmt.Errorf("tool %q is already installed", name)
	}

	if err := r.DirectInstall(entry.Spec, brewfileTypeToPkgType(entry.Type)); err != nil {
		return OptionalSetupResult{}, fmt.Errorf("install %s: %w", name, err)
	}

	if brewfilePath != "" {
		if _, err := brew.Sync(r, brewfilePath); err != nil {
			return OptionalSetupResult{}, fmt.Errorf("sync brewfile: %w", err)
		}
	}

	return OptionalSetupResult{Installed: []string{name}}, nil
}

// RunOptionalSetup presents the missing optional tools in a multi-select
// picker and installs everything the user ticks in one pass (continue-on-error
// via InstallOptionalTools). Already-installed tools are omitted — this is an
// "install what you're missing" flow, not a status view. When apps is true the
// catalog is narrowed to GUI apps (casks). When brewfilePath is non-empty, the
// Brewfile is synced after install.
func RunOptionalSetup(r brew.Runner, brewfilePath string, apps bool) (OptionalSetupResult, error) {
	entries := optionalCatalog(apps)
	if len(entries) == 0 {
		return OptionalSetupResult{}, nil
	}

	missing := missingSet(r, BrewfileOptional)
	missingEntries := lo.Filter(entries, func(e brew.BrewfileEntry, _ int) bool {
		return missing[e.Name]
	})

	if len(missingEntries) == 0 {
		noun := lo.Ternary(apps, "apps", "tools")
		uikit.Hint(fmt.Sprintf("All recommended %s are already installed.", noun))
		return OptionalSetupResult{}, nil
	}

	chosen, err := pickOptionalTools(missingEntries, apps)
	if err != nil {
		// User cancellation (esc/q/ctrl+c) is a clean no-op, not an error.
		if errors.Is(err, uikit.ErrFormAborted) {
			return OptionalSetupResult{}, nil
		}
		return OptionalSetupResult{}, err
	}
	if len(chosen) == 0 {
		return OptionalSetupResult{}, nil
	}

	return InstallOptionalTools(r, chosen, brewfilePath, false)
}

// OptionalFilter selects which optional tools to act on. Exactly one of
// All/Include/Exclude should be set; the CLI layer enforces mutex. Apps is an
// orthogonal scope modifier: when true, the catalog is narrowed to GUI apps
// (casks) before the All/Include/Exclude filter is applied.
type OptionalFilter struct {
	All     bool
	Include []string
	Exclude []string
	Apps    bool
}

// ResolveOptionalSet maps a filter onto BrewfileOptional and returns the
// entries that should be installed: all matching the filter AND currently
// missing from the system. Unknown names in Include/Exclude return an error
// listing the available tools so the user can correct.
func ResolveOptionalSet(r brew.Runner, f OptionalFilter) ([]brew.BrewfileEntry, error) {
	entries := optionalCatalog(f.Apps)
	if len(entries) == 0 {
		return nil, nil
	}

	byName := lo.SliceToMap(entries, func(e brew.BrewfileEntry) (string, brew.BrewfileEntry) {
		return e.Name, e
	})
	if unknown := unknownNames(f.Include, byName); len(unknown) > 0 {
		return nil, fmt.Errorf("unknown optional tool(s): %s (available: %s)",
			strings.Join(unknown, ", "), strings.Join(lo.Keys(byName), ", "))
	}
	if unknown := unknownNames(f.Exclude, byName); len(unknown) > 0 {
		return nil, fmt.Errorf("unknown optional tool(s) in --exclude: %s", strings.Join(unknown, ", "))
	}

	missing := missingSet(r, BrewfileOptional)

	include := lo.SliceToMap(f.Include, func(n string) (string, bool) { return n, true })
	exclude := lo.SliceToMap(f.Exclude, func(n string) (string, bool) { return n, true })

	return lo.Filter(entries, func(e brew.BrewfileEntry, _ int) bool {
		if !missing[e.Name] {
			return false // already installed
		}
		switch {
		case len(f.Include) > 0:
			return include[e.Name]
		case len(f.Exclude) > 0:
			return !exclude[e.Name]
		case f.All:
			return true
		default:
			return false
		}
	}), nil
}

// unknownNames returns names from want that don't appear in byName.
func unknownNames(want []string, byName map[string]brew.BrewfileEntry) []string {
	return lo.Filter(want, func(n string, _ int) bool { _, ok := byName[n]; return !ok })
}

// InstallOptionalTools installs each entry in sequence, continuing on per-tool
// failure so a transient brew error on one package doesn't block the rest.
// Returns a populated OptionalSetupResult; the caller decides exit code based
// on len(Failed) > 0. The Brewfile is synced once at the end (not per-tool)
// to avoid redundant snapshot work. In dry-run mode no install runs and no
// sync happens; Installed lists what would have been installed.
func InstallOptionalTools(r brew.Runner, entries []brew.BrewfileEntry, brewfilePath string, dryRun bool) (OptionalSetupResult, error) {
	if len(entries) == 0 {
		return OptionalSetupResult{DryRun: dryRun}, nil
	}

	if dryRun {
		names := lo.Map(entries, func(e brew.BrewfileEntry, _ int) string { return e.Name })
		return OptionalSetupResult{Installed: names, DryRun: true}, nil
	}

	result := OptionalSetupResult{}
	for _, e := range entries {
		if err := r.DirectInstall(e.Spec, brewfileTypeToPkgType(e.Type)); err != nil {
			result.Failed = append(result.Failed, FailedInstall{Name: e.Name, Error: err.Error()})
			continue
		}
		result.Installed = append(result.Installed, e.Name)
	}

	if brewfilePath != "" && len(result.Installed) > 0 {
		if _, err := brew.Sync(r, brewfilePath); err != nil {
			return result, fmt.Errorf("sync brewfile: %w", err)
		}
	}
	return result, nil
}

// brewfileTypeToPkgType maps Brewfile entry types ("brew", "cask", "uv")
// to the package type strings that DirectInstall expects.
func brewfileTypeToPkgType(typ string) string {
	switch typ {
	case "brew":
		return "formula"
	default:
		return typ
	}
}

// missingSet collects a system snapshot and returns a set of package names
// from content that are not installed. On error, returns a set containing
// ALL package names (assumes everything is missing) so callers don't
// incorrectly treat tools as installed. Uses [brew.CollectSnapshotForBrewfile]
// so casks installed manually (drag into /Applications, vendor .pkg) aren't
// re-offered.
func missingSet(r brew.Runner, content string) map[string]bool {
	snap, err := brew.CollectSnapshotForBrewfile(r, content)
	if err != nil {
		return allNamesSet(content)
	}
	entries := brew.ParseBrewfileEntries(content)
	missing := make(map[string]bool)
	for _, e := range entries {
		if !snap.IsInstalled(e.Type, e.Name) {
			missing[e.Name] = true
		}
	}
	return missing
}

// allNamesSet returns a set with every package name from a Brewfile marked
// as missing. Used as a safe fallback when CollectSnapshot fails.
func allNamesSet(content string) map[string]bool {
	all := brew.ParseBrewfileNames(content)
	return lo.SliceToMap(all, func(n string) (string, bool) {
		return n, true
	})
}
