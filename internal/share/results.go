package share

// results.go — [cmdutil.Result] implementations (JSON + Human output) for
// share, auth, and cloud operations.

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// CloudResult is returned by share, unshare, get.
type CloudResult struct {
	OK      bool   `json:"ok"`
	Action  string `json:"action"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
}

// JSON implements cmdutil.Result.
func (r CloudResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r CloudResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s\n", lo.Ternary(r.OK, uikit.SymOK, uikit.SymFail), r.Message)
	if r.URL != "" {
		fmt.Fprintf(&b, "\n  %s  %s\n", uikit.TUI.Dim().Render("url"), r.URL)
		fmt.Fprintf(&b, "  %s  sci cloud get <name>\n", uikit.TUI.Dim().Render("get"))
	}
	return b.String()
}

// AuthResult is returned by the auth command.
type AuthResult struct {
	OK       bool   `json:"ok"`
	Action   string `json:"action"`
	Username string `json:"username,omitempty"`
	Message  string `json:"message"`
}

// JSON implements cmdutil.Result.
func (r AuthResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r AuthResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s\n", lo.Ternary(r.OK, uikit.SymOK, uikit.SymFail), r.Message)
	if r.Action == "login" || r.Action == "status" {
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Dim().Render("Try these next:"))
		fmt.Fprintf(&b, "    sci cloud put                 Upload a file\n")
		fmt.Fprintf(&b, "    sci cloud list                List shared files\n")
	}
	return b.String()
}

// SharedListResult holds the list of the user's own shared files.
type SharedListResult struct {
	Datasets []SharedEntry `json:"datasets"`
}

// SharedEntry is a single file in SharedListResult.
type SharedEntry struct {
	Name        string `json:"name"`
	Owner       string `json:"owner,omitempty"`
	Type        string `json:"type"`
	Updated     string `json:"updated"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	Description string `json:"description,omitempty"`
}

// JSON implements cmdutil.Result.
func (r SharedListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SharedListResult) Human() string {
	if len(r.Datasets) == 0 {
		return fmt.Sprintf("  %s no files shared\n", uikit.TUI.Dim().Render("·"))
	}

	// Show owner column only when at least one entry has Owner set.
	hasOwner := lo.SomeBy(r.Datasets, func(d SharedEntry) bool {
		return d.Owner != ""
	})

	var b strings.Builder
	if hasOwner {
		fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render("owner             name                          type       size        url"))
		for _, d := range r.Datasets {
			fmt.Fprintf(&b, "  %-18s%-30s%-11s%-12s%s\n", d.Owner, d.Name, d.Type, humanize.Bytes(uint64(d.Size)), uikit.TUI.Dim().Render(d.URL))
			if d.Description != "" {
				fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render(d.Description))
			}
		}
	} else {
		fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render("name                          type       size        url"))
		for _, d := range r.Datasets {
			fmt.Fprintf(&b, "  %-30s%-11s%-12s%s\n", d.Name, d.Type, humanize.Bytes(uint64(d.Size)), uikit.TUI.Dim().Render(d.URL))
			if d.Description != "" {
				fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render(d.Description))
			}
		}
	}
	return b.String()
}

// DatasetListResult holds the list of all shared files.
type DatasetListResult struct {
	Datasets []DatasetListEntry `json:"datasets"`
}

// DatasetListEntry is a single file in DatasetListResult.
type DatasetListEntry struct {
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	Type    string `json:"type"`
	Updated string `json:"updated"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
}

// JSON implements cmdutil.Result.
func (r DatasetListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r DatasetListResult) Human() string {
	if len(r.Datasets) == 0 {
		return fmt.Sprintf("  %s no files found\n", uikit.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render("owner             name                          type       size        url"))
	for _, d := range r.Datasets {
		fmt.Fprintf(&b, "  %-18s%-30s%-11s%-12s%s\n", d.Owner, d.Name, d.Type, humanize.Bytes(uint64(d.Size)), uikit.TUI.Dim().Render(d.URL))
	}
	return b.String()
}
