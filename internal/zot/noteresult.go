package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/sciminds/sci/pkg/local"
)

// NoteReadResult is returned by `zot notes read <note-key>`.
type NoteReadResult struct {
	Note local.NoteDetail `json:"note"`
	// Markdown is the body converted out of Zotero's HTML, set by --md.
	// Opt-in: adding it unconditionally would change the --json shape under
	// every existing agent, and double the payload on a 485KB note.
	Markdown string `json:"markdown,omitempty"`
}

// noteReadJSON is the --md shape: the note's own fields, promoted by
// embedding, plus the converted body.
type noteReadJSON struct {
	local.NoteDetail
	Markdown string `json:"markdown"`
}

// JSON implements cmdutil.Result.
func (r NoteReadResult) JSON() any {
	if r.Markdown == "" {
		return r.Note
	}
	return noteReadJSON{NoteDetail: r.Note, Markdown: r.Markdown}
}

// Human implements cmdutil.Result.
func (r NoteReadResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s  %s\n",
		uikit.TUI.TextBlue().Render(r.Note.Key),
		uikit.TUI.Dim().Render("note"),
	)
	if r.Note.ParentKey != "" {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("parent:"), r.Note.ParentKey)
	}
	if r.Note.Title != "" {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("title:"), r.Note.Title)
	}
	if len(r.Note.Tags) > 0 {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("tags:"), strings.Join(r.Note.Tags, ", "))
	}
	fmt.Fprintln(&b)
	if body := noteBodyForDisplay(r.Note.Body); body != "" {
		fmt.Fprintf(&b, "%s\n", body)
	}
	return b.String()
}

// noteBodyForDisplay renders a note body for the terminal.
//
// Markdown, not stripped tags: a bare tag stripper flattens every heading,
// list marker and link, which for a 20-page extraction note discards most of
// the structure. [notemd.HTMLToMarkdown] also knows that the overwhelming
// majority of notes are already markdown and leaves those alone.
//
// Falls back to the raw body if conversion fails — showing something is
// better than showing nothing, and a display path shouldn't error.
func noteBodyForDisplay(body string) string {
	md, err := notemd.HTMLToMarkdown(body)
	if err != nil {
		return strings.TrimSpace(body)
	}
	return md
}

// RealNotesListResult is returned by `zot notes list` — the notes the user
// wrote, as distinct from the docling extractions that share Zotero's note
// storage. On the live library the two counts differ by two orders of
// magnitude (4,719 notes, 4,710 of them extractions), which is why the
// listing filters rather than reporting a total nobody means.
type RealNotesListResult struct {
	Count  int                 `json:"count"`
	Total  int                 `json:"total"`
	Offset int                 `json:"offset,omitempty"`
	Notes  []local.NoteSummary `json:"notes"`
}

// JSON implements cmdutil.Result.
func (r RealNotesListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r RealNotesListResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no notes of your own in this library\n"+
			"    %s docling extractions are the paper's text, not notes — read them with `zot read`\n",
			uikit.SymArrow, uikit.SymArrow)
	}
	total := max(r.Total, r.Count)

	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Dim().Render("your notes"))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  %s", uikit.TUI.TextBlue().Render(n.NoteKey))
		if n.ParentKey == "" {
			fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render("(standalone)"))
		} else {
			fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render(n.ParentKey))
			if n.ParentTitle != "" {
				fmt.Fprintf(&b, "  %s", n.ParentTitle)
			}
		}
		fmt.Fprintln(&b)
		if snippet := noteSnippet(n.Body); snippet != "" {
			fmt.Fprintf(&b, "    %s\n", uikit.TUI.Dim().Render(snippet))
		}
	}
	if total > r.Offset+r.Count {
		fmt.Fprintf(&b, "\n  %s showing %d-%d of %d  (pass --limit 0 for all, --offset %d for next page)\n",
			uikit.SymArrow, r.Offset+1, r.Offset+r.Count, total, r.Offset+r.Count,
		)
	} else {
		fmt.Fprintf(&b, "\n  %s %d note(s)\n", uikit.SymArrow, r.Count)
	}
	return b.String()
}
