package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/local"
)

// NotesListResult is returned by `zot notes list [parent-key]`.
// When ParentKey is empty, Notes contains all docling notes across the
// library; otherwise it's scoped to one parent.
type NotesListResult struct {
	ParentKey string                     `json:"parent_key,omitempty"`
	Count     int                        `json:"count"`
	Notes     []local.DoclingNoteSummary `json:"notes"`
}

// JSON implements cmdutil.Result.
func (r NotesListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r NotesListResult) Human() string {
	if r.Count == 0 {
		if r.ParentKey != "" {
			return fmt.Sprintf("  %s no docling notes for %s\n", uikit.SymArrow, r.ParentKey)
		}
		return fmt.Sprintf("  %s no docling notes in library\n", uikit.SymArrow)
	}
	var b strings.Builder
	if r.ParentKey != "" {
		fmt.Fprintf(&b, "\n  %s %s\n\n",
			uikit.TUI.Dim().Render("docling notes for"),
			uikit.TUI.TextBlue().Render(r.ParentKey),
		)
	} else {
		fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Dim().Render("docling notes"))
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
	fmt.Fprintf(&b, "\n  %s %d note(s)\n", uikit.SymArrow, r.Count)
	return b.String()
}

// NoteReadResult is returned by `zot notes read <note-key>`.
type NoteReadResult struct {
	Note local.NoteDetail `json:"note"`
}

// JSON implements cmdutil.Result.
func (r NoteReadResult) JSON() any { return r.Note }

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
	// Strip HTML for terminal display.
	body := stripHTML(r.Note.Body)
	if body != "" {
		fmt.Fprintf(&b, "%s\n", body)
	}
	return b.String()
}

// stripHTML is a simple tag stripper for terminal display of note
// bodies. Not a full parser — good enough for CLI output.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case inTag:
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
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
