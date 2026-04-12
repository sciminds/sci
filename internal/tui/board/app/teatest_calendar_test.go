package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	engine "github.com/sciminds/cli/internal/board"
)

// setupCalendarStore builds a Store seeded with a 12-column "calendar"
// board so tests can exercise horizontal scroll + column collapse.
// Also adds a second "current-quarter" board so tab-cycling tests have
// somewhere to cycle to.
func setupCalendarStore(t *testing.T) *engine.Store {
	t.Helper()
	obj := newFakeObjectStore()
	cachePath := filepath.Join(t.TempDir(), "board.db")
	local, err := engine.OpenLocalCache(cachePath)
	if err != nil {
		t.Fatalf("open local cache: %v", err)
	}
	t.Cleanup(func() { _ = local.Close() })

	store := engine.NewStore(obj, local, "tester")
	ctx := context.Background()

	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	cols := make([]engine.Column, len(months))
	for i, mo := range months {
		cols[i] = engine.Column{ID: strings.ToLower(mo), Title: mo}
	}
	if err := store.CreateBoard(ctx, "calendar", "Calendar", "12-month view", cols); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	// Seed one card per month so every column has content.
	for _, mo := range months {
		c := engine.Card{
			ID:       "m" + mo,
			Title:    mo + " milestone",
			Column:   strings.ToLower(mo),
			Position: 1.0,
		}
		if _, err := store.Append(ctx, "calendar", engine.OpCardAdd, engine.CardAddPayload{Card: c}); err != nil {
			t.Fatalf("seed %s: %v", mo, err)
		}
	}

	// Second board for tab-cycling tests.
	if err := store.CreateBoard(ctx, "current-quarter", "Current Quarter", "kanban",
		[]engine.Column{
			{ID: "backlog", Title: "Backlog"},
			{ID: "todo", Title: "Todo"},
			{ID: "doing", Title: "In Progress"},
			{ID: "done", Title: "Done"},
		}); err != nil {
		t.Fatalf("create current-quarter: %v", err)
	}
	qc := engine.Card{ID: "q1", Title: "Quarter-sentinel card", Column: "backlog", Position: 1}
	if _, err := store.Append(ctx, "current-quarter", engine.OpCardAdd, engine.CardAddPayload{Card: qc}); err != nil {
		t.Fatalf("seed quarter: %v", err)
	}
	return store
}

// startCalendarTeatest launches the TUI with initialBoard="calendar" and
// waits until the grid screen is rendered (the title bar contains the
// board title).
func startCalendarTeatest(t *testing.T) *teatest.TestModel {
	t.Helper()
	store := setupCalendarStore(t)
	m := NewModel(store, "calendar")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForOutput(t, tm, "Calendar")
	return tm
}

// TestCalendarOpensOnFirstMonth: at launch, gridScroll sits at 0 so the
// first columns are visible.
func TestCalendarOpensOnFirstMonth(t *testing.T) {
	tm := startCalendarTeatest(t)
	fm := finalModel(t, tm)
	if fm.current.ID != "calendar" {
		t.Fatalf("expected calendar board, got %q", fm.current.ID)
	}
	if fm.gridScroll != 0 {
		t.Errorf("gridScroll=%d, want 0", fm.gridScroll)
	}
}

// TestCalendarNavigateScrollsWindow: pressing l past the initial visible
// window scrolls gridScroll forward.
func TestCalendarNavigateScrollsWindow(t *testing.T) {
	tm := startCalendarTeatest(t)
	for i := 0; i < 10; i++ {
		sendKey(tm, "l")
	}
	fm := finalModel(t, tm)
	if fm.cur.col != 10 {
		t.Errorf("cur.col=%d, want 10", fm.cur.col)
	}
	if fm.gridScroll == 0 {
		t.Errorf("gridScroll=0, expected scroll after 10 l-presses")
	}
}

// TestCalendarCollapseToggle: pressing c toggles collapse for the focused
// column. Pressing again expands it.
func TestCalendarCollapseToggle(t *testing.T) {
	tm := startCalendarTeatest(t)
	sendKey(tm, "l") // col 1 = feb
	sendKey(tm, "c")
	fm := finalModel(t, tm)
	if !fm.collapsed["feb"] {
		t.Errorf("expected feb collapsed, got %v", fm.collapsed)
	}
}

// TestTabCyclesToNextBoard: pressing tab on the grid screen loads the
// next board (alphabetical). With boards=[calendar, current-quarter],
// tab from calendar should land on current-quarter.
func TestTabCyclesToNextBoard(t *testing.T) {
	tm := startCalendarTeatest(t)
	sendSpecial(tm, tea.KeyTab)
	waitForOutput(t, tm, "BACKLOG")
	fm := finalModel(t, tm)
	if fm.current.ID != "current-quarter" {
		t.Errorf("expected current-quarter, got %q", fm.current.ID)
	}
}

// TestShiftTabCyclesToPrevBoard: shift+tab wraps to the previous board.
// From calendar (index 0) shift+tab wraps to current-quarter (index 1).
func TestShiftTabCyclesToPrevBoard(t *testing.T) {
	tm := startCalendarTeatest(t)
	tm.Send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	waitForOutput(t, tm, "BACKLOG")
	fm := finalModel(t, tm)
	if fm.current.ID != "current-quarter" {
		t.Errorf("expected current-quarter after shift+tab wrap, got %q", fm.current.ID)
	}
}

// TestTabWrapsFromLastToFirst: tab from the last board wraps to the
// first. Open current-quarter, press tab → calendar.
func TestTabWrapsFromLastToFirst(t *testing.T) {
	store := setupCalendarStore(t)
	m := NewModel(store, "current-quarter")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForOutput(t, tm, "BACKLOG")
	sendSpecial(tm, tea.KeyTab)
	waitForOutput(t, tm, "JAN")
	fm := finalModel(t, tm)
	if fm.current.ID != "calendar" {
		t.Errorf("expected calendar after tab wrap, got %q", fm.current.ID)
	}
}

// TestCalendarExpandAll: pressing C clears all collapse state.
func TestCalendarExpandAll(t *testing.T) {
	tm := startCalendarTeatest(t)
	sendKey(tm, "c") // collapse jan
	sendKey(tm, "l")
	sendKey(tm, "c") // collapse feb
	sendKey(tm, "C") // expand all
	fm := finalModel(t, tm)
	if len(fm.collapsed) != 0 {
		t.Errorf("expected empty collapsed map, got %v", fm.collapsed)
	}
}
