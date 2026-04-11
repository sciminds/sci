package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// SetupResult is returned by `zot setup` / `sci zot setup`.
type SetupResult struct {
	OK        bool   `json:"ok"`
	LibraryID string `json:"library_id,omitempty"`
	DataDir   string `json:"data_dir,omitempty"`
	Message   string `json:"message"`
}

func (r SetupResult) JSON() any { return r }
func (r SetupResult) Human() string {
	var b strings.Builder
	sym := ui.SymOK
	if !r.OK {
		sym = ui.SymFail
	}
	fmt.Fprintf(&b, "  %s %s\n", sym, r.Message)
	if r.OK {
		if r.LibraryID != "" {
			fmt.Fprintf(&b, "    library: %s\n", r.LibraryID)
		}
		if r.DataDir != "" {
			fmt.Fprintf(&b, "    data dir: %s\n", r.DataDir)
		}
	}
	return b.String()
}
