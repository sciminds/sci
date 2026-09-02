package zot

import (
	"fmt"
	"strings"
)

// SetupInput gathers all fields Setup accepts. Structured so optional
// knobs (per-library tuning, future group handling) can be added without
// churning every call site.
type SetupInput struct {
	APIKey          string // optional — enables writes; the CLI verifies it before passing it here
	UserID          string // paired with APIKey — the numeric user ID /keys/current reported for it
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
//
// Credential handling: a blank APIKey means "keep whatever is stored", not
// "log out" — re-running setup to move the data directory must not cost the
// user their key. Forgetting it is `--clear`'s job.
func Setup(in SetupInput) (*SetupResult, error) {
	if err := ValidateDataDir(in.DataDir); err != nil {
		return nil, err
	}

	apiKey, userID := in.APIKey, in.UserID
	if apiKey == "" {
		if existing, err := LoadConfig(); err == nil && existing != nil {
			apiKey, userID = existing.APIKey, existing.UserID
		}
	}
	// A key is only ever stored beside the user ID it was verified against;
	// a caller passing one without the other has skipped the verification.
	if apiKey != "" {
		if err := ValidateUserID(userID); err != nil {
			return nil, fmt.Errorf("api key given without a verified user ID: %w", err)
		}
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
		APIKey:          apiKey,
		UserID:          userID,
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
		Writes:      cfg.HasCredential(),
		Message:     "zot configured",
	}, nil
}

// ClearSetup clears the saved configuration — the flag behind it is
// `sci zot setup --clear`: sci forgets where the library is and the API key
// it held. Keys sci does not own survive it (see [ClearConfig]).
func ClearSetup() (*SetupResult, error) {
	if err := ClearConfig(); err != nil {
		return nil, err
	}
	return &SetupResult{OK: true, Message: "zot configuration cleared"}, nil
}
