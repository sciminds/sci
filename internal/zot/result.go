package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// JSON implements cmdutil.Result.
func (c Config) JSON() any { return c }

// Human implements cmdutil.Result.
func (c Config) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s zot config\n", uikit.SymOK)
	fmt.Fprintf(&b, "    user id:  %s\n", c.UserID)
	if c.SharedGroupID != "" {
		fmt.Fprintf(&b, "    shared:   %s (groupID %s)\n", c.SharedGroupName, c.SharedGroupID)
	}
	fmt.Fprintf(&b, "    data dir: %s\n", c.DataDir)
	fmt.Fprintf(&b, "    api key:  %s\n", c.APIKey)
	return b.String()
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
