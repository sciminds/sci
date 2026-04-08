package share

// results.go — [cmdutil.Result] implementations (JSON + Human output) for
// share, auth, and cloud operations.

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/ui"
)

// CloudResult is returned by share, unshare, get.
type CloudResult struct {
	OK      bool   `json:"ok"`
	Action  string `json:"action"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
}

func (r CloudResult) JSON() any { return r }
func (r CloudResult) Human() string {
	var b strings.Builder
	if r.OK {
		fmt.Fprintf(&b, "  %s %s\n", ui.SymOK, r.Message)
	} else {
		fmt.Fprintf(&b, "  %s %s\n", ui.SymFail, r.Message)
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "\n  %s  %s\n", ui.TUI.Dim().Render("url"), r.URL)
		fmt.Fprintf(&b, "  %s  sci cloud get <name>\n", ui.TUI.Dim().Render("get"))
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

func (r AuthResult) JSON() any { return r }
func (r AuthResult) Human() string {
	var b strings.Builder
	if r.OK {
		fmt.Fprintf(&b, "  %s %s\n", ui.SymOK, r.Message)
	} else {
		fmt.Fprintf(&b, "  %s %s\n", ui.SymFail, r.Message)
	}
	if r.Action == "login" || r.Action == "status" {
		fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Dim().Render("Try these next:"))
		fmt.Fprintf(&b, "    sci cloud share              Share a file publicly\n")
		fmt.Fprintf(&b, "    sci cloud share --private     Share to private bucket\n")
		fmt.Fprintf(&b, "    sci cloud list                List your shared files\n")
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
	Type        string `json:"type"`
	Updated     string `json:"updated"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	Description string `json:"description,omitempty"`
}

func (r SharedListResult) JSON() any { return r }
func (r SharedListResult) Human() string {
	if len(r.Datasets) == 0 {
		return fmt.Sprintf("  %s no files shared\n", ui.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render("name                          type       size        url"))
	for _, d := range r.Datasets {
		fmt.Fprintf(&b, "  %-30s%-11s%-12s%s\n", d.Name, d.Type, humanize.Bytes(uint64(d.Size)), ui.TUI.Dim().Render(d.URL))
		if d.Description != "" {
			fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render(d.Description))
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

func (r DatasetListResult) JSON() any { return r }
func (r DatasetListResult) Human() string {
	if len(r.Datasets) == 0 {
		return fmt.Sprintf("  %s no files found\n", ui.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render("owner             name                          type       size        url"))
	for _, d := range r.Datasets {
		fmt.Fprintf(&b, "  %-18s%-30s%-11s%-12s%s\n", d.Owner, d.Name, d.Type, humanize.Bytes(uint64(d.Size)), ui.TUI.Dim().Render(d.URL))
	}
	return b.String()
}
