package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/sciminds/sci/pkg/local"
)

// NotesListResult is returned by `zot notes list [parent-key]`.
// When ParentKey is empty, Notes contains all docling notes across the
// library; otherwise it's scoped to one parent.
//
// Total is the unpaginated count; Count is len(Notes) — i.e. how many
// the page actually contains. Offset is the starting index of the slice
// in the original list. When Total > Offset+Count the human renderer
// emits a paginate footer so the agent (or human) knows there's more.
type NotesListResult struct {
	ParentKey string                     `json:"parent_key,omitempty"`
	Count     int                        `json:"count"`
	Total     int                        `json:"total"`
	Offset    int                        `json:"offset,omitempty"`
	Notes     []local.DoclingNoteSummary `json:"notes"`
}

// JSON implements cmdutil.Result.
func (r NotesListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NotesListResult) Human() string {
	total := r.Total
	if total == 0 {
		total = r.Count
	}
	if r.Count == 0 {
		if r.ParentKey != "" {
			return fmt.Sprintf("  %s no extraction for %s\n", uikit.SymArrow, r.ParentKey)
		}
		return fmt.Sprintf("  %s no extractions in library\n", uikit.SymArrow)
	}
	var b strings.Builder
	if r.ParentKey != "" {
		fmt.Fprintf(&b, "\n  %s %s\n\n",
			uikit.TUI.Dim().Render("extractions for"),
			uikit.TUI.TextBlue().Render(r.ParentKey),
		)
	} else {
		fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Dim().Render("extractions"))
	}
	for _, n := range r.Notes {
		snippet := noteSnippet(n.Body)
		fmt.Fprintf(&b, "  %s  %s",
			uikit.TUI.TextBlue().Render(n.NoteKey),
			uikit.TUI.Dim().Render(n.ParentKey),
		)
		if n.ParentTitle != "" {
			fmt.Fprintf(&b, "  %s", n.ParentTitle)
		}
		fmt.Fprintln(&b)
		if snippet != "" {
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

// NoteAddResult is returned by `zot notes add <parent-key>`.
type NoteAddResult struct {
	ParentKey   string        `json:"parent_key"`
	PDFName     string        `json:"pdf_name"`
	NoteKey     string        `json:"note_key"`
	Action      string        `json:"action"`
	ToolVersion string        `json:"tool_version,omitempty"`
	Duration    time.Duration `json:"duration_ns,omitempty"`
}

// JSON implements cmdutil.Result.
func (r NoteAddResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NoteAddResult) Human() string {
	var b strings.Builder
	if r.Action == string(actionSkip) {
		fmt.Fprintf(&b, "  %s skipped %s — docling note already exists\n", uikit.SymArrow, r.PDFName)
		return b.String()
	}
	fmt.Fprintf(&b, "  %s created note %s for %s\n", uikit.SymOK, r.NoteKey, r.PDFName)
	if r.ToolVersion != "" && r.Duration > 0 {
		fmt.Fprintf(&b, "      %s in %s\n", r.ToolVersion, r.Duration.Truncate(time.Second))
	}
	return b.String()
}

// NoteUpdateResult is returned by `zot notes update <parent-key>`.
type NoteUpdateResult struct {
	ParentKey   string        `json:"parent_key"`
	PDFName     string        `json:"pdf_name"`
	NoteKey     string        `json:"note_key"`
	ToolVersion string        `json:"tool_version,omitempty"`
	Duration    time.Duration `json:"duration_ns,omitempty"`
}

// JSON implements cmdutil.Result.
func (r NoteUpdateResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NoteUpdateResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s updated note %s for %s\n", uikit.SymOK, r.NoteKey, r.PDFName)
	if r.ToolVersion != "" && r.Duration > 0 {
		fmt.Fprintf(&b, "      %s in %s\n", r.ToolVersion, r.Duration.Truncate(time.Second))
	}
	return b.String()
}

// NoteDeleteResult is returned by `zot notes delete`.
// ParentKey is empty for the --all bulk path.
type NoteDeleteResult struct {
	ParentKey string            `json:"parent_key,omitempty"`
	Total     int               `json:"total"`
	Trashed   []string          `json:"trashed,omitempty"`
	Failed    map[string]string `json:"failed,omitempty"`
	// UntaggedParents lists parent keys whose has-markdown tag was
	// removed because their last docling note was trashed. Empty in the
	// no-op case (no notes existed). The next extract-lib --apply will
	// re-tag them if a new docling note is created.
	UntaggedParents []string `json:"untagged_parents,omitempty"`
}

// JSON implements cmdutil.Result.
func (r NoteDeleteResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NoteDeleteResult) Human() string {
	var b strings.Builder
	if len(r.Trashed) == 0 && len(r.Failed) == 0 {
		if r.ParentKey != "" {
			fmt.Fprintf(&b, "  %s no docling notes found for %s\n", uikit.SymArrow, r.ParentKey)
		} else {
			fmt.Fprintf(&b, "  %s no docling notes found in library\n", uikit.SymArrow)
		}
		return b.String()
	}
	for _, k := range r.Trashed {
		fmt.Fprintf(&b, "  %s trashed note %s\n", uikit.SymOK, k)
	}
	if len(r.Failed) > 0 {
		keys := slices.Sorted(maps.Keys(r.Failed))
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s %s: %s\n", uikit.SymFail, k, r.Failed[k])
		}
	}
	if len(r.UntaggedParents) > 0 {
		fmt.Fprintf(&b, "  %s removed has-markdown from %d parent(s)\n", uikit.SymArrow, len(r.UntaggedParents))
	}
	return b.String()
}

// RealNotesListResult is returned by `zot notes list` — the notes the user
// wrote, as distinct from [NotesListResult], which lists docling
// extractions under `zot content list`.
//
// The two are separate result types rather than one with a flag because
// they answer different questions and their empty states must not be
// confusable: "no notes in library" and "no extractions in library" are
// very different facts about a library, and on the live one the counts
// differ by two orders of magnitude.
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
			"    %s docling extractions live under `sci zot content list`\n",
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
