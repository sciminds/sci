package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/pkg/local"
)

// LinkListResult is returned by `zot link list <key>`.
// Labels for the far ends live on Relations.Titles rather than in a map of
// this result's own: `item read` carries the same set on local.Item, and
// one place to look for a title beats two shapes that mean the same thing.
type LinkListResult struct {
	Key       string                `json:"key"`
	Relations local.ItemRelationSet `json:"relations"`
	// Remote records that this came from the Zotero Web API rather than
	// the local mirror — relations written seconds ago are only there.
	Remote bool `json:"remote,omitempty"`
}

// JSON implements cmdutil.Result.
func (r LinkListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LinkListResult) Human() string {
	if len(r.Relations.Related) == 0 && len(r.Relations.Other) == 0 {
		return fmt.Sprintf("  %s no relations on %s\n", uikit.SymArrow, r.Key)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s %s\n", uikit.TUI.Dim().Render("relations for"),
		uikit.TUI.TextBlue().Render(r.Key))
	if r.Remote {
		fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render("(live from Zotero)"))
	}

	if len(r.Relations.Related) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Dim().Render("related"))
		for _, k := range r.Relations.Related {
			writeLinkRow(&b, "  ", k, r.Relations.Titles[k])
		}
	}
	// Zotero's own predicates render under their real names and last, so
	// nothing suggests `link rm` should be pointed at them.
	for _, pred := range slices.Sorted(maps.Keys(r.Relations.Other)) {
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.TUI.Dim().Render(pred),
			uikit.TUI.Dim().Render("(Zotero-managed)"))
		for _, k := range r.Relations.Other[pred] {
			writeLinkRow(&b, "  ", k, r.Relations.Titles[k])
		}
	}
	return b.String()
}

// writeLinkRow renders one "key  title" row of a relation listing. indent
// is passed in because the same row appears at two nesting depths — flush
// under `link list`'s own header, one level deeper inside `item read`'s
// field block, beside attachments.
func writeLinkRow(b *strings.Builder, indent, key, title string) {
	fmt.Fprintf(b, "%s%s", indent, uikit.TUI.TextBlue().Render(key))
	if title != "" {
		fmt.Fprintf(b, "  %s", title)
	}
	fmt.Fprintln(b)
}
