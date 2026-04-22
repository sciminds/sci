package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// SavedSearchCondition is the cmdutil-facing shape of a single Zotero
// saved-search condition. Wire-equivalent to client.SearchCondition but
// re-declared in the zot package so cmdutil consumers don't pull the
// generated client into their import graph.
type SavedSearchCondition struct {
	Condition string `json:"condition"`
	Operator  string `json:"operator"`
	Value     string `json:"value"`
}

// SavedSearch is the cmdutil-facing shape of a Zotero saved search.
// Hydrated by the api wrappers (CreateSavedSearch / GetSavedSearch /
// ListSavedSearches) and embedded in WriteResult.Data and the result
// types below.
type SavedSearch struct {
	Key        string                 `json:"key"`
	Version    int                    `json:"version"`
	Name       string                 `json:"name"`
	Conditions []SavedSearchCondition `json:"conditions"`
	Deleted    bool                   `json:"deleted,omitempty"`
}

// SavedSearchListResult is returned for `zot saved-search list`.
type SavedSearchListResult struct {
	Count    int           `json:"count"`
	Searches []SavedSearch `json:"searches"`
}

// JSON implements cmdutil.Result.
func (r SavedSearchListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SavedSearchListResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no saved searches\n", uikit.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, s := range r.Searches {
		nameSuffix := ""
		if s.Deleted {
			nameSuffix = " " + uikit.TUI.Dim().Render("(trashed)")
		}
		fmt.Fprintf(&b, "  %s  %s%s %s\n",
			uikit.TUI.TextBlue().Render(s.Key),
			s.Name,
			nameSuffix,
			uikit.TUI.Dim().Render(fmt.Sprintf("(%d condition(s))", len(s.Conditions))),
		)
	}
	fmt.Fprintf(&b, "\n  %s %d saved search(es)\n", uikit.SymArrow, r.Count)
	return b.String()
}

// SavedSearchResult is returned for `zot saved-search show`.
type SavedSearchResult struct {
	Search SavedSearch `json:"search"`
}

// JSON implements cmdutil.Result.
func (r SavedSearchResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SavedSearchResult) Human() string {
	s := r.Search
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s %s\n",
		uikit.TUI.TextBlueBold().Render(s.Name),
		uikit.TUI.Dim().Render(s.Key),
		uikit.TUI.Dim().Render(fmt.Sprintf("v%d", s.Version)),
	)
	if s.Deleted {
		fmt.Fprintf(&b, "    %s\n", uikit.TUI.Dim().Render("trashed"))
	}
	for _, c := range s.Conditions {
		val := c.Value
		if val == "" {
			val = uikit.TUI.Dim().Render("(empty)")
		}
		fmt.Fprintf(&b, "    %s %s %s\n",
			uikit.TUI.Dim().Render(c.Condition),
			c.Operator,
			val,
		)
	}
	return b.String()
}
