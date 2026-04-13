package lab

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// SetupResult is returned by sci lab setup.
type SetupResult struct {
	OK      bool   `json:"ok"`
	User    string `json:"user"`
	Message string `json:"message"`
}

// JSON implements cmdutil.Result.
func (r SetupResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SetupResult) Human() string {
	sym := lo.Ternary(r.OK, ui.SymOK, ui.SymFail)
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s\n", sym, r.Message)
	return b.String()
}

// LsResult is returned by sci lab ls.
type LsResult struct {
	Path string `json:"path"`
	Raw  string `json:"listing"`
}

// JSON implements cmdutil.Result.
func (r LsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LsResult) Human() string {
	if r.Raw == "" {
		return fmt.Sprintf("  %s empty directory: %s\n", ui.TUI.Dim().Render("·"), r.Path)
	}
	return r.Raw
}
