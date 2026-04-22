package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// NoteItemReadResult is returned by `zot item note read KEY`. Body is the
// raw HTML as stored in Zotero; Human output strips tags to plain text for
// terminal readability unless ShowHTML is set (from the --html flag).
type NoteItemReadResult struct {
	Key          string   `json:"key"`
	ParentItem   string   `json:"parent_item,omitempty"`
	Collections  []string `json:"collections,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Body         string   `json:"body"`
	DateAdded    string   `json:"date_added,omitempty"`
	DateModified string   `json:"date_modified,omitempty"`

	// ShowHTML flips Human output from stripped-text to raw HTML. Not
	// JSON-serialized — it's a display knob, not part of the payload.
	ShowHTML bool `json:"-"`
}

// JSON implements cmdutil.Result.
func (r NoteItemReadResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NoteItemReadResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s  %s\n",
		uikit.TUI.TextBlue().Render(r.Key),
		uikit.TUI.Dim().Render("note"),
	)
	if r.ParentItem != "" {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("parent:"), r.ParentItem)
	}
	if len(r.Collections) > 0 {
		fmt.Fprintf(&b, "  %s %s\n",
			uikit.TUI.Dim().Render("collections:"),
			strings.Join(r.Collections, ", "),
		)
	}
	if len(r.Tags) > 0 {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("tags:"), strings.Join(r.Tags, ", "))
	}
	fmt.Fprintln(&b)
	body := r.Body
	if !r.ShowHTML {
		body = stripHTML(body)
	}
	if body != "" {
		fmt.Fprintf(&b, "%s\n", body)
	}
	return b.String()
}

// NoteItemListEntry is one row in NoteItemListResult. Body is always raw
// HTML — the Human rendering derives a snippet from it.
type NoteItemListEntry struct {
	Key  string   `json:"key"`
	Body string   `json:"body"`
	Tags []string `json:"tags,omitempty"`
}

// NoteItemListResult is returned by `zot item note list PARENT`. Scoped
// to the children of one parent item (child notes only; attachments and
// other child types are filtered out upstream).
type NoteItemListResult struct {
	ParentKey string              `json:"parent_key"`
	Count     int                 `json:"count"`
	Notes     []NoteItemListEntry `json:"notes"`
}

// JSON implements cmdutil.Result.
func (r NoteItemListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NoteItemListResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no notes attached to %s\n", uikit.SymArrow, r.ParentKey)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s %s\n\n",
		uikit.TUI.Dim().Render("notes on"),
		uikit.TUI.TextBlue().Render(r.ParentKey),
	)
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  %s", uikit.TUI.TextBlue().Render(n.Key))
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render(strings.Join(n.Tags, ", ")))
		}
		fmt.Fprintln(&b)
		if snippet := noteSnippet(n.Body); snippet != "" {
			fmt.Fprintf(&b, "    %s\n", uikit.TUI.Dim().Render(snippet))
		}
	}
	fmt.Fprintf(&b, "\n  %s %d note(s)\n", uikit.SymArrow, r.Count)
	return b.String()
}
