package app

import (
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// TestViewAtZeroSize ensures the TUI Model can render View() before any
// WindowSizeMsg arrives (width=0, height=0). Bubble Tea calls View()
// immediately on startup — if View() panics at zero dimensions, users
// see a crash on launch.
func TestViewAtZeroSize(t *testing.T) {
	t.Run("emptyDB", func(t *testing.T) {
		m := minimalModel()
		_ = m.View() // must not panic
	})

	t.Run("withTabs", func(t *testing.T) {
		m := minimalModel()
		m.tabs = []Tab{*makeTab([]string{"id", "name"}, [][]string{{"1", "alice"}})}
		_ = m.View() // must not panic
	})

	t.Run("helpVisible", func(t *testing.T) {
		m := minimalModel()
		m.helpVisible = true
		_ = m.View() // must not panic
	})

	t.Run("withNotePreview", func(t *testing.T) {
		m := minimalModel()
		m.tabs = []Tab{*makeTab([]string{"notes"}, [][]string{{"some text"}})}
		m.notePreview = &notePreviewState{Text: "preview text", Title: "notes"}
		_ = m.View() // must not panic
	})

	t.Run("withTableList", func(t *testing.T) {
		m := minimalModel()
		m.tableList = &tableListState{
			Tables: []tableListEntry{{Name: "users", Rows: 3, Columns: 2}},
		}
		_ = m.View() // must not panic
	})

	t.Run("withColumnPicker", func(t *testing.T) {
		m := minimalModel()
		tab := makeTab([]string{"id", "name", "email"}, [][]string{{"1", "alice", "a@b.c"}})
		tab.Specs[2].HideOrder = 1 // hide "email"
		m.tabs = []Tab{*tab}
		m.columnPicker = &columnPickerState{Cursor: 0}
		_ = m.View() // must not panic
	})
}

// TestNewModelMarksViewsReadOnly ensures SQL views are read-only tabs,
// while regular tables remain editable.
func TestNewModelMarksViewsReadOnly(t *testing.T) {
	store := setupViewTestDB(t)

	m, err := NewModel(store, "test.db", false)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// Verify the viewLister was detected.
	if m.viewLister == nil {
		t.Fatal("viewLister is nil, expected non-nil for Store")
	}

	// The first tab is fully loaded. Find it by name.
	var tableTab, viewTab *Tab
	for i := range m.tabs {
		switch m.tabs[i].Name {
		case "penguins":
			tableTab = &m.tabs[i]
		case "penguin_summary":
			viewTab = &m.tabs[i]
		}
	}

	if tableTab == nil {
		t.Fatal("penguins tab not found")
	}
	if viewTab == nil {
		t.Fatal("penguin_summary tab not found")
	}

	// The first tab alphabetically is "example" — it's loaded and should be editable.
	if m.tabs[0].ReadOnly {
		t.Errorf("first tab %q is ReadOnly, expected editable", m.tabs[0].Name)
	}

	// View tab may be a stub (not loaded yet) — if loaded, verify read-only.
	if viewTab.Loaded && !viewTab.ReadOnly {
		t.Error("penguin_summary view tab is editable, expected read-only")
	}
}

// TestViewTabReadOnlyAfterLazyLoad ensures view tabs become read-only when
// lazy-loaded via tab switching.
func TestViewTabReadOnlyAfterLazyLoad(t *testing.T) {
	store := setupViewTestDB(t)

	m, err := NewModel(store, "test.db", false)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// Find the view tab index.
	viewIdx := -1
	for i, tab := range m.tabs {
		if tab.Name == "penguin_summary" {
			viewIdx = i
			break
		}
	}
	if viewIdx < 0 {
		t.Fatal("penguin_summary tab not found")
	}

	// Simulate switching to the view tab (triggers async load).
	cmd := m.switchToTab(viewIdx)
	if cmd == nil {
		// Already loaded — check read-only.
		if !m.tabs[viewIdx].ReadOnly {
			t.Error("view tab loaded but not read-only")
		}
		return
	}

	// Execute the async load command to get the tabLoadedMsg.
	msg := cmd().(tabLoadedMsg)
	m.handleTabLoaded(msg)

	if !m.tabs[viewIdx].Loaded {
		t.Fatal("view tab not loaded after tabLoadedMsg")
	}
	if !m.tabs[viewIdx].ReadOnly {
		t.Error("view tab should be read-only after lazy load")
	}
}

func TestViewAtZeroSizeWithViewTab(t *testing.T) {
	m := minimalModel()
	tab := makeTab([]string{"species", "cnt"}, [][]string{{"Adelie", "3"}})
	tab.ReadOnly = true
	tab.Name = "my_view"
	m.tabs = []Tab{*tab}
	_ = m.View() // must not panic
}

func setupViewTestDB(t *testing.T) *data.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Set up schema using a temporary connection.
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	schema := []string{
		"CREATE TABLE penguins (species TEXT, island TEXT, bill REAL, year INTEGER)",
		`INSERT INTO penguins VALUES
			('Adelie','Torgersen',39.1,2007),('Adelie','Biscoe',37.8,2007),
			('Gentoo','Biscoe',NULL,2008),('Chinstrap','Dream',49.6,2009),
			('Gentoo','Biscoe',45.2,NULL),('Adelie','Dream',40.9,2007)`,
		"CREATE TABLE example (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL)",
		"INSERT INTO example VALUES (1,'Alice',95.5),(2,'Bob',NULL),(3,'Carol',88.0)",
		`CREATE VIEW penguin_summary AS
			SELECT species, COUNT(*) AS cnt FROM penguins GROUP BY species`,
	}

	for _, stmt := range schema {
		if _, err := store.Exec(stmt); err != nil {
			t.Fatalf("setup schema: %v", err)
		}
	}

	// Force TableNames to populate the views map.
	if _, err := store.TableNames(); err != nil {
		t.Fatalf("TableNames: %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	// Verify it implements ViewLister.
	if _, ok := data.DataStore(store).(data.ViewLister); !ok {
		t.Fatal("Store does not implement ViewLister")
	}

	return store
}

// minimalModel returns a *Model with the minimum non-nil fields required by View().
func minimalModel() *Model {
	h := ui.NewHelp()
	h.ShowAll = true

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = ui.TUI.FgAccent()

	return &Model{
		zones:   zone.New(),
		styles:  ui.TUI,
		help:    help.New(),
		spinner: s,
		mode:    modeNormal,
	}
}
