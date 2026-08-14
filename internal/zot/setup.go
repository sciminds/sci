package zot

import (
	"fmt"
	"strings"
)

// SetupInput gathers all fields Setup accepts. Structured so optional
// knobs (per-library tuning, future group handling) can be added without
// churning every call site.
type SetupInput struct {
	SharedGroupID   string // optional — explicit pick when GroupProbe returns multiple
	SharedGroupName string // optional — display name, paired with SharedGroupID
	DataDir         string // required

	// GroupProbe is injected by the CLI layer and called during setup to
	// auto-detect the shared group. It reads the local Zotero database's
	// `groups` table — there is no account to ask. Tests pass a fake.
	// When nil, setup skips auto-detection and relies on the explicit
	// SharedGroupID/Name inputs (or leaves shared unconfigured).
	GroupProbe GroupProbeFunc
}

// Setup validates inputs, writes config to disk, and returns a SetupResult.
// All interactive prompting happens in the CLI layer (internal/zot/cli) —
// this function is the pure business-logic entry point.
//
// Shared-group handling:
//   - If SharedGroupID is non-empty, it is written through as-is (explicit pick).
//   - Else if GroupProbe is non-nil, read the local `groups` table:
//   - 1 group → auto-populate SharedGroupID + SharedGroupName
//   - 0 groups → leave shared unconfigured (the library has no groups)
//   - ≥2 groups → error, listing the options
//   - Else (no probe, no explicit pick) → shared stays unconfigured.
func Setup(in SetupInput) (*SetupResult, error) {
	if err := ValidateDataDir(in.DataDir); err != nil {
		return nil, err
	}

	sharedID := in.SharedGroupID
	sharedName := in.SharedGroupName
	if sharedID == "" && in.GroupProbe != nil {
		groups, err := in.GroupProbe()
		if err != nil {
			return nil, fmt.Errorf("read Zotero groups: %w", err)
		}
		switch len(groups) {
		case 0:
			// Leave shared blank; the library has no groups.
		case 1:
			sharedID = groups[0].ID
			sharedName = groups[0].Name
		default:
			names := make([]string, 0, len(groups))
			for _, g := range groups {
				names = append(names, g.Name)
			}
			return nil, fmt.Errorf("this Zotero library holds multiple groups (%s) — pass --shared-group-id to pick one", strings.Join(names, ", "))
		}
	}

	cfg := &Config{
		SharedGroupID:   sharedID,
		SharedGroupName: sharedName,
		DataDir:         in.DataDir,
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	return &SetupResult{
		OK:          true,
		DataDir:     in.DataDir,
		SharedGroup: sharedName,
		Message:     "zot configured",
	}, nil
}

// Logout clears the saved configuration. There is no credential to
// forget — this only makes sci stop knowing where the library is.
func Logout() (*SetupResult, error) {
	if err := ClearConfig(); err != nil {
		return nil, err
	}
	return &SetupResult{OK: true, Message: "zot configuration cleared"}, nil
}
