package brew

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// AddResult is returned by Add.
type AddResult struct {
	Package string `json:"package"`
	Type    string `json:"type,omitempty"`
}

// JSON implements cmdutil.Result.
func (r AddResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r AddResult) Human() string {
	if r.Type != "" {
		return fmt.Sprintf("Added %s (%s)\n", r.Package, r.Type)
	}
	return fmt.Sprintf("Added %s\n", r.Package)
}

// RemoveResult is returned by Remove.
type RemoveResult struct {
	Package string `json:"package"`
	Type    string `json:"type,omitempty"`
}

// JSON implements cmdutil.Result.
func (r RemoveResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r RemoveResult) Human() string {
	if r.Type != "" {
		return fmt.Sprintf("Removed %s (%s)\n", r.Package, r.Type)
	}
	return fmt.Sprintf("Removed %s\n", r.Package)
}

// InstallResult is returned by Install.
type InstallResult struct {
	Output string `json:"output"`
}

// JSON implements cmdutil.Result.
func (r InstallResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r InstallResult) Human() string {
	if r.Output == "" {
		return "Everything up to date.\n"
	}
	return r.Output
}

// UpdateResult is returned by Update.
type UpdateResult struct {
	Outdated      []OutdatedPackage `json:"outdated"`
	CheckOnly     bool              `json:"check_only"`
	UpgradeOutput string            `json:"upgrade_output,omitempty"`
}

// JSON implements cmdutil.Result.
func (r UpdateResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r UpdateResult) Human() string {
	if len(r.Outdated) == 0 {
		return "Everything is up to date.\n"
	}

	var b strings.Builder
	if r.CheckOnly {
		fmt.Fprintf(&b, "%d outdated package(s):\n\n", len(r.Outdated))
	} else {
		fmt.Fprintf(&b, "Upgraded %d package(s):\n\n", len(r.Outdated))
	}
	for _, pkg := range r.Outdated {
		arrow := uikit.TUI.TextPink().Render(" → ")
		version := uikit.TUI.TextPink().Render(pkg.InstalledVersion) + arrow + pkg.CurrentVersion
		fmt.Fprintf(&b, "  %s %s\n", pkg.Name, version)
	}
	return b.String()
}

// SyncResult is returned by Sync.
type SyncResult struct {
	Added        int      `json:"added"`
	Removed      int      `json:"removed"`
	AddedNames   []string `json:"added_names,omitempty"`
	RemovedNames []string `json:"removed_names,omitempty"`
}

// JSON implements cmdutil.Result.
func (r SyncResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SyncResult) Human() string {
	if r.Added == 0 && r.Removed == 0 {
		return ""
	}
	var parts []string
	if r.Added > 0 {
		parts = append(parts, fmt.Sprintf("added %d", r.Added))
	}
	if r.Removed > 0 {
		parts = append(parts, fmt.Sprintf("removed %d", r.Removed))
	}
	return fmt.Sprintf("Synced Brewfile (%s)\n", strings.Join(parts, ", "))
}

// ListResult is returned by List.
type ListResult struct {
	Packages []string `json:"packages"`
	Type     string   `json:"type,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ListResult) Human() string {
	if len(r.Packages) == 0 {
		return "No packages found.\n"
	}
	return strings.Join(r.Packages, "\n") + "\n"
}
