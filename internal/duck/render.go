// render.go — Human() rendering for every verb result. All tabular output
// flows through uikit.RenderTable so `sci db` shares the same bordered,
// terminal-width-aware, ellipsis-truncating look as the rest of the CLI. The
// dplyr-style glimpse is the one exception: a one-line-per-column listing
// rather than a grid.
//
// Rendering reads uikit.TermWidth() at call time. On a TTY cells are
// truncated to fit; when output is piped (width 0) full content is emitted so
// downstream tools see complete cells — structured consumers use --json.

package duck

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// Human renders the column listing: (column, type) normally, plus declared
// type and a fallback note for SQLite sources where a declared type couldn't
// be honored.
func (r *ColsResult) Human() string {
	if len(r.Columns) == 0 {
		return ""
	}
	showDeclared := lo.SomeBy(r.Columns, func(c ColumnInfo) bool { return c.Declared != "" })
	if showDeclared {
		rows := lo.Map(r.Columns, func(c ColumnInfo, _ int) []string {
			note := ""
			if c.FailingCells > 0 {
				note = fmt.Sprintf("fallback: %d cell(s) did not cast to %s", c.FailingCells, c.Declared)
			}
			return []string{c.Name, c.Type, c.Declared, note}
		})
		return uikit.RenderTable([]string{"column", "type", "declared", "note"}, rows, tableOpts())
	}
	rows := lo.Map(r.Columns, func(c ColumnInfo, _ int) []string {
		return []string{c.Name, c.Type}
	})
	return uikit.RenderTable([]string{"column", "type"}, rows, tableOpts())
}

// Human renders the rows as a bordered table in projection order.
func (r *RowsResult) Human() string {
	if len(r.Columns) == 0 {
		return "(no columns)\n"
	}
	rows := lo.Map(r.Rows, func(row map[string]any, _ int) []string {
		return lo.Map(r.Columns, func(col string, _ int) string {
			return formatCell(row[col])
		})
	})
	return uikit.RenderTable(r.Columns, rows, tableOpts())
}

// Human renders the dplyr::glimpse-style preview: a Rows/Columns header
// followed by one line per column — `name <type> v1, "v2", …` — truncated to
// the terminal width (full content when piped).
func (r *GlimpseResult) Human() string {
	if len(r.Columns) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rows: %d\n", r.RowCount)
	fmt.Fprintf(&b, "Columns: %d\n", len(r.Columns))

	nameW := lo.Max(lo.Map(r.Columns, func(c GlimpseColumn, _ int) int { return len(c.Name) }))
	width := uikit.TermWidth()
	for _, c := range r.Columns {
		samples := strings.Join(lo.Map(c.Samples, func(s any, _ int) string {
			return formatSample(s)
		}), ", ")
		line := fmt.Sprintf("%s %-*s <%s> %s",
			uikit.TUI.TextDim().Render("$"), nameW, c.Name, c.Type, samples)
		b.WriteString(uikit.Truncate(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

// Human renders the (rows, columns) shape as a small table.
func (r *ShapeResult) Human() string {
	return uikit.RenderTable(
		[]string{"rows", "columns"},
		[][]string{{fmt.Sprintf("%d", r.Rows), fmt.Sprintf("%d", r.Columns)}},
		uikit.TableOptions{Width: uikit.TermWidth(), RightAlign: []bool{true, true}},
	)
}

// summarizeHeaders is the fixed column order of the SUMMARIZE table.
var summarizeHeaders = []string{
	"column", "type", "min", "max", "approx_unique",
	"avg", "std", "q25", "q50", "q75", "count", "null_percentage",
}

// Human renders per-column statistics. Count-like columns are right-aligned.
func (r *SummarizeResult) Human() string {
	if len(r.Columns) == 0 {
		return ""
	}
	rows := lo.Map(r.Columns, func(c SummarizeColumn, _ int) []string {
		return []string{
			c.Name, c.Type, c.Min, c.Max, fmt.Sprintf("%d", c.ApproxUnique),
			c.Avg, c.Std, c.Q25, c.Q50, c.Q75, fmt.Sprintf("%d", c.Count), c.NullPercentage,
		}
	})
	rightAlign := lo.Map(summarizeHeaders, func(h string, _ int) bool {
		return h == "approx_unique" || h == "count" || h == "null_percentage"
	})
	return uikit.RenderTable(summarizeHeaders, rows, uikit.TableOptions{
		Width:      uikit.TermWidth(),
		RightAlign: rightAlign,
	})
}

// tableOpts is the default rendering width (live terminal, 0 when piped).
func tableOpts() uikit.TableOptions {
	return uikit.TableOptions{Width: uikit.TermWidth()}
}

// formatCell renders a duckdb -json scalar for a table cell. NULLs become
// empty (matching duckdb's box mode); json.Number preserves duckdb's exact
// numeric text; STRUCT/LIST/MAP values arrive as nested JSON and are
// re-encoded compactly.
func formatCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		out, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(out)
	}
}

// formatSample renders a glimpse sample value dplyr-style: strings are quoted
// and flattened to one line, NULLs show as NA, everything else reuses
// formatCell.
func formatSample(v any) string {
	switch x := v.(type) {
	case nil:
		return "NA"
	case string:
		flat := strings.ReplaceAll(strings.ReplaceAll(x, "\n", " "), "\r", "")
		return fmt.Sprintf("%q", flat)
	default:
		return formatCell(v)
	}
}
