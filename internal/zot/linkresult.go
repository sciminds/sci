package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/internal/zot/link"
	"github.com/sciminds/sci/pkg/local"
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

// LinkSuggestResult is the cmdutil.Result shell around a *link.Result. One
// type for both modes — the dry run and the apply that follows it render
// identically apart from the header and the per-row glyph, so diffing them
// is trivial.
type LinkSuggestResult struct {
	Result *link.Result `json:"result"`
}

// JSON implements cmdutil.Result.
func (r LinkSuggestResult) JSON() any { return r.Result }

// Human implements cmdutil.Result.
func (r LinkSuggestResult) Human() string {
	if r.Result == nil {
		return ""
	}
	var b strings.Builder

	header := "Link suggestions (dry-run)"
	if r.Result.Applied {
		header = "Link suggestions (applied)"
	}
	fmt.Fprintf(&b, "\n  %s %s\n", uikit.TUI.TextBlueBold().Render(header),
		uikit.TUI.TextBlue().Render(r.Result.NoteKey))

	t := r.Result.Totals
	if len(r.Result.Suggestions) == 0 {
		fmt.Fprintf(&b, "  %s no references found in this note\n", uikit.SymArrow)
		return b.String()
	}
	fmt.Fprintf(&b, "  %s %d proposed, %d already linked, %d unresolved\n\n",
		uikit.TUI.Dim().Render("·"), t.Proposed, t.AlreadyLinked, t.Unresolved)

	linkedByKey := lo.KeyBy(r.Result.Outcomes, func(oc link.Outcome) string { return oc.Key })
	for _, s := range r.Result.Suggestions {
		writeSuggestionRow(&b, s, linkedByKey)
	}

	if r.Result.Applied {
		fmt.Fprintf(&b, "\n  %s %d linked  %s %d failed\n",
			uikit.SymOK, t.Succeeded, uikit.SymFail, t.Failed)
	} else if t.Proposed > 0 {
		fmt.Fprintf(&b, "\n  %s dry-run only — pass %s to write the relations\n",
			uikit.SymArrow, uikit.TUI.TextBlue().Render("--apply"))
	}
	return b.String()
}

// writeSuggestionRow renders one suggestion: a glyph for its fate, then the
// key + title (or, for an unresolved reference, the raw text and why it
// didn't land), then how it was cited.
func writeSuggestionRow(b *strings.Builder, s link.Suggestion, outcomes map[string]link.Outcome) {
	icon := uikit.TUI.Dim().Render("·")
	switch s.Status {
	case link.StatusAlreadyLinked:
		icon = uikit.SymOK
	case link.StatusUnresolved:
		icon = uikit.SymFail
	}
	if oc, ok := outcomes[s.Key]; ok && s.Status == link.StatusProposed {
		icon = uikit.SymFail
		if oc.Linked {
			icon = uikit.SymOK
		}
	}

	subject := uikit.TUI.Dim().Render(s.Ref)
	if s.Key != "" {
		subject = uikit.TUI.TextBlue().Render(s.Key)
		if s.Title != "" {
			subject += "  " + s.Title
		}
	}
	fmt.Fprintf(b, "    %s  %-14s %s\n", icon,
		uikit.TUI.Warn().Render(string(s.Status)), subject)

	detail := s.Reason
	if detail == "" {
		detail = "via " + strings.Join(lo.Map(s.Via, func(k bib.RefKind, _ int) string {
			return string(k)
		}), ", ")
	}
	if len(s.Candidates) > 0 {
		detail += " — " + strings.Join(s.Candidates, ", ")
	}
	fmt.Fprintf(b, "        %s\n", uikit.TUI.Dim().Render(detail))

	if oc, ok := outcomes[s.Key]; ok && oc.Error != "" {
		fmt.Fprintf(b, "        %s %s\n", uikit.SymFail, uikit.TUI.Fail().Render(oc.Error))
	}
}
