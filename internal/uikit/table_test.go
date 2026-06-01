package uikit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// maxLineWidth returns the widest visible line in s (ANSI-aware).
func maxLineWidth(s string) int {
	w := 0
	for line := range strings.SplitSeq(s, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			w = lw
		}
	}
	return w
}

func TestRenderTableFullWidthWhenZero(t *testing.T) {
	long := strings.Repeat("x", 200)
	out := RenderTable([]string{"id", "body"}, [][]string{{"1", long}}, TableOptions{Width: 0})
	if !strings.Contains(out, long) {
		t.Errorf("Width:0 should preserve full content; missing the 200-char cell.\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("Width:0 should not truncate; found ellipsis.\n%s", out)
	}
}

func TestRenderTableTruncatesToWidth(t *testing.T) {
	long := strings.Repeat("x", 200)
	const width = 40
	out := RenderTable([]string{"id", "body"}, [][]string{{"1", long}}, TableOptions{Width: width})
	if got := maxLineWidth(out); got > width {
		t.Errorf("rendered width = %d, want <= %d\n%s", got, width, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("expected ellipsis when truncating to width %d\n%s", width, out)
	}
	if strings.Contains(out, long) {
		t.Errorf("the full 200-char cell should not survive truncation\n%s", out)
	}
}

func TestRenderTableKeepsHeadersAndCells(t *testing.T) {
	out := RenderTable(
		[]string{"name", "type"},
		[][]string{{"id", "INTEGER"}, {"title", "VARCHAR"}},
		TableOptions{Width: 0},
	)
	for _, want := range []string{"name", "type", "id", "INTEGER", "title", "VARCHAR"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestRenderTableEmpty(t *testing.T) {
	if got := RenderTable(nil, nil, TableOptions{}); got != "" {
		t.Errorf("empty table should render empty string, got %q", got)
	}
}
