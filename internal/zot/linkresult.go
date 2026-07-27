package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/local"
)

// LinkResult is returned by `zot link` and `zot link rm`.
type LinkResult struct {
	// A and B are the two item keys, in the order the user gave them.
	A string `json:"a"`
	B string `json:"b"`
	// Removed distinguishes `link rm` from `link` in the JSON shape, so an
	// agent doesn't have to infer the verb from the command it ran.
	Removed bool `json:"removed,omitempty"`
	// Titles maps each key to its title when one was resolvable, so the
	// human line can say what was linked rather than just which keys.
	Titles map[string]string `json:"titles,omitempty"`
}

// JSON implements cmdutil.Result.
func (r LinkResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LinkResult) Human() string {
	verb := "linked"
	if r.Removed {
		verb = "unlinked"
	}
	return fmt.Sprintf("\n  %s %s %s %s %s\n%s%s",
		uikit.SymOK,
		verb,
		uikit.TUI.TextBlue().Render(r.A),
		uikit.TUI.Dim().Render("↔"),
		uikit.TUI.TextBlue().Render(r.B),
		linkTitleLine(r.Titles, r.A),
		linkTitleLine(r.Titles, r.B),
	)
}

func linkTitleLine(titles map[string]string, key string) string {
	title := titles[key]
	if title == "" {
		return ""
	}
	return fmt.Sprintf("    %s %s\n",
		uikit.TUI.Dim().Render(key), uikit.TUI.Dim().Render(title))
}

// LinkListResult is returned by `zot link list <key>`.
type LinkListResult struct {
	Key       string                `json:"key"`
	Relations local.ItemRelationSet `json:"relations"`
	// Titles maps every referenced key to its title where resolvable. A
	// bare list of 8-char keys is unreadable; the title is what tells the
	// user (or agent) whether the link is the one they meant.
	Titles map[string]string `json:"titles,omitempty"`
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
			writeLinkRow(&b, k, r.Titles[k])
		}
	}
	// Zotero's own predicates render under their real names and last, so
	// nothing suggests `link rm` should be pointed at them.
	for pred, keys := range r.Relations.Other {
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.TUI.Dim().Render(pred),
			uikit.TUI.Dim().Render("(Zotero-managed)"))
		for _, k := range keys {
			writeLinkRow(&b, k, r.Titles[k])
		}
	}
	return b.String()
}

func writeLinkRow(b *strings.Builder, key, title string) {
	fmt.Fprintf(b, "  %s", uikit.TUI.TextBlue().Render(key))
	if title != "" {
		fmt.Fprintf(b, "  %s", title)
	}
	fmt.Fprintln(b)
}
