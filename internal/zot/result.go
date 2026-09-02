package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
)

// JSON implements cmdutil.Result. The stored shape with the API key masked
// to a recognisable prefix: `setup --json` is a status report, and a status
// report is not where a credential should be readable in full.
func (c Config) JSON() any {
	c.APIKey = MaskAPIKey(c.APIKey)
	return c
}

// Human implements cmdutil.Result.
func (c Config) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s zot config\n", uikit.SymOK)
	fmt.Fprintf(&b, "    data dir: %s\n", c.DataDir)
	if c.SharedGroupID != "" {
		fmt.Fprintf(&b, "    shared:   %s (groupID %s)\n", c.SharedGroupName, c.SharedGroupID)
	}
	switch {
	case c.HasCredential():
		fmt.Fprintf(&b, "    api key:  %s (user %s) — writes on\n", MaskAPIKey(c.APIKey), c.UserID)
	case c.APIKey != "":
		fmt.Fprintf(&b, "    api key:  %s — stored but unverified; re-run `sci zot setup --api-key <key>`\n", MaskAPIKey(c.APIKey))
	default:
		fmt.Fprintf(&b, "    api key:  (not set) — reads only; add one with `sci zot setup --api-key <key>`\n")
	}
	return b.String()
}

// MaskAPIKey renders a key as a short recognisable prefix, never in full —
// four characters is enough to tell two keys apart and too few to be worth
// stealing. An empty key stays empty so callers can keep their own
// "(not set)" wording.
func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	return key[:min(4, len(key))] + "…"
}

// SetupResult is returned by `zot setup` / `sci zot setup`.
type SetupResult struct {
	OK          bool   `json:"ok"`
	DataDir     string `json:"data_dir,omitempty"`
	SharedGroup string `json:"shared_group,omitempty"`
	Writes      bool   `json:"writes"` // an API key is stored, so the write verbs will run
	Message     string `json:"message"`
}

// JSON implements cmdutil.Result.
func (r SetupResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SetupResult) Human() string {
	var b strings.Builder
	sym := uikit.SymOK
	if !r.OK {
		sym = uikit.SymFail
	}
	fmt.Fprintf(&b, "  %s %s\n", sym, r.Message)
	if r.OK {
		if r.DataDir != "" {
			fmt.Fprintf(&b, "    data dir: %s\n", r.DataDir)
		}
		if r.SharedGroup != "" {
			fmt.Fprintf(&b, "    shared:   %s\n", r.SharedGroup)
		}
		if r.DataDir != "" {
			writes := "off (reads only — add an API key with `sci zot setup --api-key <key>`)"
			if r.Writes {
				writes = "on (API key stored)"
			}
			fmt.Fprintf(&b, "    writes:   %s\n", writes)
		}
	}
	return b.String()
}
