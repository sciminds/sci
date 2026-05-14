package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// JSON implements cmdutil.Result.
//
// Returns a sanitized view of Config: API keys are replaced with
// has_* booleans so the rendered JSON cannot expose credentials in
// logs, CI artifacts, or screen captures. SaveConfig keeps the full
// shape on disk under 0600 — only this user-facing surface is masked.
func (c Config) JSON() any {
	return configView{
		HasAPIKey:         c.APIKey != "",
		UserID:            c.UserID,
		SharedGroupID:     c.SharedGroupID,
		SharedGroupName:   c.SharedGroupName,
		DataDir:           c.DataDir,
		OpenAlexEmail:     c.OpenAlexEmail,
		HasOpenAlexAPIKey: c.OpenAlexAPIKey != "",
	}
}

// configView is the secret-stripped shape emitted by Config.JSON.
type configView struct {
	HasAPIKey         bool   `json:"has_api_key"`
	UserID            string `json:"user_id"`
	SharedGroupID     string `json:"shared_group_id,omitempty"`
	SharedGroupName   string `json:"shared_group_name,omitempty"`
	DataDir           string `json:"data_dir"`
	OpenAlexEmail     string `json:"openalex_email,omitempty"`
	HasOpenAlexAPIKey bool   `json:"has_openalex_api_key,omitempty"`
}

// Human implements cmdutil.Result.
func (c Config) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s zot config\n", uikit.SymOK)
	fmt.Fprintf(&b, "    user id:  %s\n", c.UserID)
	if c.SharedGroupID != "" {
		fmt.Fprintf(&b, "    shared:   %s (groupID %s)\n", c.SharedGroupName, c.SharedGroupID)
	}
	fmt.Fprintf(&b, "    data dir: %s\n", c.DataDir)
	if c.APIKey != "" {
		fmt.Fprintf(&b, "    api key:  %s\n", maskKey(c.APIKey))
	}
	if c.OpenAlexAPIKey != "" {
		fmt.Fprintf(&b, "    openalex: %s\n", maskKey(c.OpenAlexAPIKey))
	}
	return b.String()
}

// maskKey returns "****" plus the last four characters of key, so the
// user can confirm which key is loaded without exposing material that
// would let a shoulder-surfer reconstruct it. Short keys mask fully.
func maskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// SetupResult is returned by `zot setup` / `sci zot setup`.
type SetupResult struct {
	OK      bool   `json:"ok"`
	UserID  string `json:"user_id,omitempty"`
	DataDir string `json:"data_dir,omitempty"`
	Message string `json:"message"`
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
		if r.UserID != "" {
			fmt.Fprintf(&b, "    user id: %s\n", r.UserID)
		}
		if r.DataDir != "" {
			fmt.Fprintf(&b, "    data dir: %s\n", r.DataDir)
		}
	}
	return b.String()
}
