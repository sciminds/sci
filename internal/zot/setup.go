package zot

import (
	"fmt"
	"strings"
)

// SetupInput gathers all fields Setup accepts. Structured so optional
// knobs (OpenAlex credentials today, future per-library tuning later)
// can be added without churning every call site.
type SetupInput struct {
	APIKey          string // required
	UserID          string // required — numeric Zotero user ID
	SharedGroupID   string // optional — explicit pick when GroupProbe returns multiple
	SharedGroupName string // optional — display name, paired with SharedGroupID
	DataDir         string // required
	OpenAlexEmail   string // optional
	OpenAlexAPIKey  string // optional

	// GroupProbe is injected by the CLI layer and called during setup to
	// auto-detect the shared group for the account. Tests pass a fake.
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
//   - Else if GroupProbe is non-nil, probe the Zotero API:
//   - 1 group → auto-populate SharedGroupID + SharedGroupName
//   - 0 groups → leave shared unconfigured (account has no groups)
//   - ≥2 groups → error, listing the options
//   - Else (no probe, no explicit pick) → shared stays unconfigured.
func Setup(in SetupInput) (*SetupResult, error) {
	if err := ValidateAPIKey(in.APIKey); err != nil {
		return nil, err
	}
	if err := ValidateUserID(in.UserID); err != nil {
		return nil, err
	}
	if err := ValidateDataDir(in.DataDir); err != nil {
		return nil, err
	}

	sharedID := in.SharedGroupID
	sharedName := in.SharedGroupName
	if sharedID == "" && in.GroupProbe != nil {
		groups, err := in.GroupProbe(in.APIKey, in.UserID)
		if err != nil {
			return nil, fmt.Errorf("probe Zotero groups: %w", err)
		}
		switch len(groups) {
		case 0:
			// Leave shared blank; account has no groups.
		case 1:
			sharedID = groups[0].ID
			sharedName = groups[0].Name
		default:
			names := make([]string, 0, len(groups))
			for _, g := range groups {
				names = append(names, g.Name)
			}
			return nil, fmt.Errorf("account has multiple groups (%s) — pass --shared-group-id to pick one", strings.Join(names, ", "))
		}
	}

	cfg := &Config{
		APIKey:          in.APIKey,
		UserID:          in.UserID,
		SharedGroupID:   sharedID,
		SharedGroupName: sharedName,
		DataDir:         in.DataDir,
		OpenAlexEmail:   in.OpenAlexEmail,
		OpenAlexAPIKey:  in.OpenAlexAPIKey,
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	return &SetupResult{
		OK:      true,
		UserID:  in.UserID,
		DataDir: in.DataDir,
		Message: "zot configured",
	}, nil
}

// Logout clears saved credentials.
func Logout() (*SetupResult, error) {
	if err := ClearConfig(); err != nil {
		return nil, err
	}
	return &SetupResult{OK: true, Message: "zot credentials cleared"}, nil
}
