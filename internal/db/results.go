package db

// results.go — [cmdutil.Result] implementations (JSON + Human output) for
// database subcommands: info, tables, and mutations.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// InfoResult holds database metadata and table listing.
type InfoResult struct {
	DBPath string       `json:"db_path"`
	SizeMB float64      `json:"size_mb"`
	Tables []TableEntry `json:"tables"`
}

// JSON implements cmdutil.Result.
func (r InfoResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r InfoResult) Human() string {
	nTables, nViews, nVirtual := r.counts()
	var b strings.Builder
	fmt.Fprintf(&b, "  %s  %s\n", ui.TUI.Dim().Render("path"), r.DBPath)
	fmt.Fprintf(&b, "  %s  %.2f MB  %s  %s\n",
		ui.TUI.Dim().Render("size"), r.SizeMB,
		ui.TUI.Dim().Render("·"), r.summaryLabel(nTables, nViews, nVirtual),
	)
	if len(r.Tables) > 0 {
		b.WriteByte('\n')
		writeTableRows(&b, r.Tables, nViews > 0, nVirtual > 0)
	}
	return b.String()
}

func (r InfoResult) counts() (tables, views, virtual int) {
	for _, t := range r.Tables {
		switch {
		case t.IsView:
			views++
		case t.IsVirtual:
			virtual++
		default:
			tables++
		}
	}
	return
}

func (r InfoResult) summaryLabel(nTables, nViews, nVirtual int) string {
	var parts []string
	if nTables > 0 {
		label := "tables"
		if nTables == 1 {
			label = "table"
		}
		parts = append(parts, ui.TUI.Accent().Render(fmt.Sprintf("%d %s", nTables, label)))
	}
	if nViews > 0 {
		label := "views"
		if nViews == 1 {
			label = "view"
		}
		parts = append(parts, ui.TUI.FgSecondary().Render(fmt.Sprintf("%d %s", nViews, label)))
	}
	if nVirtual > 0 {
		label := "virtual"
		parts = append(parts, ui.TUI.FgMuted().Render(fmt.Sprintf("%d %s", nVirtual, label)))
	}
	if len(parts) == 0 {
		return "0 tables"
	}
	return strings.Join(parts, "  "+ui.TUI.Dim().Render("·")+"  ")
}

// TablesResult holds table summary information.
type TablesResult struct {
	Tables []TableEntry `json:"tables"`
}

// TableEntry is a single table or view in TablesResult.
type TableEntry struct {
	Name      string `json:"name"`
	Rows      int    `json:"rows"`
	Columns   int    `json:"columns"`
	IsView    bool   `json:"is_view,omitempty"`
	IsVirtual bool   `json:"is_virtual,omitempty"`
}

// JSON implements cmdutil.Result.
func (r TablesResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r TablesResult) Human() string {
	hasViews := lo.SomeBy(r.Tables, func(t TableEntry) bool { return t.IsView })
	hasVirtual := lo.SomeBy(r.Tables, func(t TableEntry) bool { return t.IsVirtual })
	var b strings.Builder
	writeTableRows(&b, r.Tables, hasViews, hasVirtual)
	if b.Len() == 0 {
		fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render("no tables"))
	}
	return b.String()
}

// writeTableRows renders a formatted table of entries with dynamic column widths.
func writeTableRows(b *strings.Builder, entries []TableEntry, hasViews, hasVirtual bool) {
	if len(entries) == 0 {
		return
	}

	// Build header first so we can factor its visual width into column sizing.
	var headerParts []string
	headerParts = append(headerParts, ui.TUI.Accent().Render("table"))
	if hasViews {
		headerParts = append(headerParts, ui.TUI.FgSecondary().Render("view"))
	}
	if hasVirtual {
		headerParts = append(headerParts, ui.TUI.FgMuted().Render("virtual"))
	}
	var header string
	if len(headerParts) == 1 {
		header = "table"
	} else {
		header = strings.Join(headerParts, ui.TUI.Dim().Render(" / "))
	}

	// Compute column widths from data and header.
	nameW := visLen(header)
	rowsW := len("rows")
	colsW := len("columns")
	for _, t := range entries {
		if n := len(t.Name); n > nameW {
			nameW = n
		}
		if n := len(strconv.Itoa(t.Rows)); n > rowsW {
			rowsW = n
		}
		if n := len(strconv.Itoa(t.Columns)); n > colsW {
			colsW = n
		}
	}
	nameW += 2 // padding between columns
	rowsW += 2

	headerPad := nameW - visLen(header)
	if headerPad < 0 {
		headerPad = 0
	}
	fmt.Fprintf(b, "  %s%s%s   %s\n",
		header, strings.Repeat(" ", headerPad),
		ui.TUI.Dim().Render(padRight("rows", rowsW)),
		ui.TUI.Dim().Render("columns"),
	)

	// Rows.
	for _, t := range entries {
		var name string
		switch {
		case t.IsView:
			name = ui.TUI.FgSecondary().Render(t.Name)
		case t.IsVirtual:
			name = ui.TUI.FgMuted().Render(t.Name)
		default:
			name = ui.TUI.Accent().Render(t.Name)
		}
		namePad := nameW - visLen(name)
		if namePad < 0 {
			namePad = 0
		}
		fmt.Fprintf(b, "  %s%s%s   %d\n",
			name, strings.Repeat(" ", namePad),
			padRight(strconv.Itoa(t.Rows), rowsW),
			t.Columns,
		)
	}
}

// padRight pads s with spaces on the right to width w.
func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// visLen returns the visible length of s (stripping ANSI escape sequences).
func visLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

// MutationResult is returned by add, delete, rename, convert.
type MutationResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// JSON implements cmdutil.Result.
func (r MutationResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r MutationResult) Human() string {
	if r.OK {
		return fmt.Sprintf("  %s %s\n", ui.SymOK, r.Message)
	}
	return fmt.Sprintf("  %s %s\n", ui.SymFail, r.Message)
}
