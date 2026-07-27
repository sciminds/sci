package app

// yank_test.go — unit coverage for the y / Y system-clipboard copy path.
// The system clipboard is faked via uikit.SetClipboardRunnerForTest, so no
// test ever shells out to pbcopy/xclip.

import (
	"strings"
	"sync"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
)

// makeYankModel builds a normal-mode model over an id/name tab.
func makeYankModel(rows [][]string) *Model {
	tab := makeTab([]string{"id", "name"}, rows)
	tab.Table.SetHeight(20)
	return &Model{
		tabs:   []Tab{*tab},
		active: 0,
		mode:   modeNormal,
		styles: uikit.TUI,
	}
}

// captureClipboard swaps in a fake clipboard runner for the duration of the
// test and returns a getter for the last payload written. The getter is
// mutex-guarded so teatest models (which run Update on their own goroutine)
// can use it too.
func captureClipboard(t *testing.T) func() string {
	t.Helper()
	var (
		mu      sync.Mutex
		payload string
	)
	restore := uikit.SetClipboardRunnerForTest(func(_ string, _ []string, s string) error {
		mu.Lock()
		defer mu.Unlock()
		payload = s
		return nil
	})
	t.Cleanup(restore)
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return payload
	}
}

// 1. An ordinary cell copies its raw value — no trimming.
func TestYankCellCopiesValueVerbatim(t *testing.T) {
	m := makeYankModel([][]string{{"1", "  spaced  "}})
	m.tabs[0].ColCursor = 1
	got := captureClipboard(t)

	m.yankCell()

	if got() != "  spaced  " {
		t.Errorf("payload = %q, want %q", got(), "  spaced  ")
	}
	if m.status.Kind != statusInfo {
		t.Errorf("status kind = %v, want statusInfo (text %q)", m.status.Kind, m.status.Text)
	}
	if !strings.Contains(m.status.Text, "Copied cell") {
		t.Errorf("status = %q, want it to mention %q", m.status.Text, "Copied cell")
	}
}

// 2. A NULL cell copies the empty string and says so.
func TestYankCellNull(t *testing.T) {
	m := makeYankModel([][]string{{"1", "placeholder"}})
	m.tabs[0].ColCursor = 1
	m.tabs[0].CellRows[0][1].Null = true
	got := captureClipboard(t)

	m.yankCell()

	if got() != "" {
		t.Errorf("payload = %q, want empty string", got())
	}
	if !strings.Contains(m.status.Text, "NULL") {
		t.Errorf("status = %q, want it to mention NULL", m.status.Text)
	}
}

// 3. An empty (but non-NULL) cell copies "" and reports it.
func TestYankCellEmpty(t *testing.T) {
	m := makeYankModel([][]string{{"1", ""}})
	m.tabs[0].ColCursor = 1
	got := captureClipboard(t)

	m.yankCell()

	if got() != "" {
		t.Errorf("payload = %q, want empty string", got())
	}
	if m.status.Text != "Copied empty cell" {
		t.Errorf("status = %q, want %q", m.status.Text, "Copied empty cell")
	}
}

// 4. yankRow's payload is exactly formatRowsTSV for the cursor row.
func TestYankRowMatchesFormatRowsTSV(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}, {"2", "bob"}})
	m.tabs[0].Table.SetCursor(1)
	got := captureClipboard(t)

	m.yankRow()

	want := formatRowsTSV(m.effectiveTab(), []int{1})
	if got() != want {
		t.Errorf("payload = %q, want %q", got(), want)
	}
	if want != "id\tname\n2\tbob\n" {
		t.Errorf("formatRowsTSV = %q, want %q", want, "id\tname\n2\tbob\n")
	}
	if !strings.Contains(m.status.Text, "Copied row") {
		t.Errorf("status = %q, want it to mention %q", m.status.Text, "Copied row")
	}
}

// 5. A failing clipboard tool surfaces as a status error, not a silent no-op.
func TestYankCellRunnerErrorSetsStatus(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}})
	m.tabs[0].ColCursor = 1

	restore := uikit.SetClipboardRunnerForTest(func(string, []string, string) error {
		return stubErr("clipboard failure")
	})
	defer restore()

	m.yankCell()

	if m.status.Kind != statusError {
		t.Errorf("status kind = %v, want statusError (text %q)", m.status.Kind, m.status.Text)
	}
}
