package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
)

// JSON implements cmdutil.Result. The config carries no credential, so this
// is the whole stored shape.
func (c Config) JSON() any { return c }

// Human implements cmdutil.Result.
func (c Config) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s zot config\n", uikit.SymOK)
	fmt.Fprintf(&b, "    data dir: %s\n", c.DataDir)
	if c.SharedGroupID != "" {
		fmt.Fprintf(&b, "    shared:   %s (groupID %s)\n", c.SharedGroupName, c.SharedGroupID)
	}
	return b.String()
}

// SetupResult is returned by `zot setup` / `sci zot setup`.
type SetupResult struct {
	OK          bool   `json:"ok"`
	DataDir     string `json:"data_dir,omitempty"`
	SharedGroup string `json:"shared_group,omitempty"`
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
	}
	return b.String()
}
