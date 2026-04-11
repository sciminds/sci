package app

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
)

const (
	testTermW = 100
	testTermH = 30
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

// ── Shared helpers ──────────────────────────────────────────────────────

// setupTeatestDB creates a test database with a few tables and returns a store.
func setupTeatestDB(t *testing.T) *data.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "teatest.db")
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`,
		`INSERT INTO users VALUES
			(1, 'Alice', 'alice@example.com'),
			(2, 'Bob', 'bob@example.com'),
			(3, 'Carol', 'carol@example.com')`,
		`CREATE TABLE products (id INTEGER PRIMARY KEY, title TEXT, price REAL)`,
		`INSERT INTO products VALUES
			(1, 'Widget', 9.99),
			(2, 'Gadget', 24.50),
			(3, 'Doohickey', 3.75)`,
	}
	for _, stmt := range stmts {
		if _, err := store.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if _, err := store.TableNames(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// newTeatestModel creates a *Model ready for teatest.
func newTeatestModel(t *testing.T) *Model {
	t.Helper()
	store := setupTeatestDB(t)
	m, err := NewModel(store, "teatest.db", false)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// newReadOnlyTeatestModel creates a forceRO=true model.
func newReadOnlyTeatestModel(t *testing.T) *Model {
	t.Helper()
	store := setupTeatestDB(t)
	m, err := NewModel(store, "teatest.db", true)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// newEmptyTeatestModel creates a model with no tables.
func newEmptyTeatestModel(t *testing.T) *Model {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TableNames(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m, err := NewModel(store, "empty.db", false)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// newTeatestModelWithSchema creates a model with custom SQL statements.
func newTeatestModelWithSchema(t *testing.T, stmts []string) (*Model, *data.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range stmts {
		if _, err := store.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if _, err := store.TableNames(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m, err := NewModel(store, "custom.db", false)
	if err != nil {
		t.Fatal(err)
	}
	return m, store
}

// sendKey sends a single rune key message.
func sendKey(tm *teatest.TestModel, key string) {
	runes := []rune(key)
	if len(runes) == 1 {
		tm.Send(tea.KeyPressMsg{Code: runes[0], Text: key})
	} else {
		tm.Send(tea.KeyPressMsg{Text: key})
	}
}

// sendSpecial sends a special key (tea.KeyEnter, tea.KeyEscape, etc).
func sendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

// finalModel sends Ctrl+C, waits for exit, and returns the *Model.
func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	return final.(*Model)
}

// waitForTable waits until the output contains "products" (rendered after init).
func waitForTable(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("products"))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
}

// waitForOutput waits for substr to appear in the test model's output.
func waitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
}

// startTeatest is a convenience that creates a model + test model + waits for render.
func startTeatest(t *testing.T) (*teatest.TestModel, *data.Store) {
	t.Helper()
	store := setupTeatestDB(t)
	m, err := NewModel(store, "teatest.db", false)
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)
	return tm, store
}

// ── Basic integration tests ─────────────────────────────────────────────

// TestTeatestBasicRender verifies the TUI renders table data on startup.
//
// Mirrors the upstream teatest.TestApp pattern: capture the full output
// stream from program start through clean teardown and golden-compare
// it. We use io.TeeReader to snapshot bytes into our own buffer while
// teatest.WaitFor drains the underlying bytes.Buffer — without the tee,
// the drained bytes would be lost to FinalOutput, and the golden would
// only contain the teardown tail (which is nondeterministic under race
// because bubbletea's startup terminal probes can arrive late).
func TestTeatestBasicRender(t *testing.T) {
	m := newTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))

	var transcript bytes.Buffer
	tee := io.TeeReader(tm.Output(), &transcript)
	teatest.WaitFor(t, tee, func(bts []byte) bool {
		return bytes.Contains(bts, []byte("products"))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))

	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	rest, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(testFinal)))
	if err != nil {
		t.Fatal(err)
	}
	transcript.Write(rest)

	teatest.RequireEqualOutput(t, transcript.Bytes())
}

// TestTeatestCursorNavigation verifies j/k navigation moves the cursor.
func TestTeatestCursorNavigation(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "j")
	sendKey(tm, "j")
	sendKey(tm, "k")

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab after navigation")
	}
	if got := tab.Table.Cursor(); got != 1 {
		t.Errorf("cursor = %d, want 1", got)
	}
}

// TestTeatestTabSwitch verifies tab/shift+tab switches between tables.
func TestTeatestTabSwitch(t *testing.T) {
	tm, _ := startTeatest(t)

	sendSpecial(tm, tea.KeyTab)
	// Wait for the second tab to render after async load.
	waitForOutput(t, tm, "users")

	fm := finalModel(t, tm)

	if fm.active != 1 {
		t.Errorf("active tab = %d, want 1", fm.active)
	}
	if !fm.tabs[1].Loaded {
		t.Error("second tab should be loaded after switch")
	}
}

// TestTeatestEditMode verifies entering and exiting edit mode.
func TestTeatestEditMode(t *testing.T) {
	tm, _ := startTeatest(t)

	// Enter edit mode with 'i'.
	sendKey(tm, "i")

	// Exit edit mode.
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal (%d)", fm.mode, modeNormal)
	}
}

// TestTeatestVisualMode verifies entering visual mode and selecting rows.
func TestTeatestVisualMode(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, "j")

	fm := finalModel(t, tm)

	if fm.mode != modeVisual {
		t.Errorf("mode = %d, want modeVisual (%d)", fm.mode, modeVisual)
	}
	if fm.visual == nil {
		t.Fatal("visual state is nil in visual mode")
	}
}

// TestTeatestSearchOverlay verifies the search overlay opens and closes.
func TestTeatestSearchOverlay(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Alice")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.search != nil {
		t.Error("search overlay should be closed after Esc")
	}
}

// TestTeatestTableListOverlay verifies the table list overlay opens with 't'.
func TestTeatestTableListOverlay(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "t")

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Error("table list overlay should be open after pressing 't'")
	}
}

// TestTeatestHelpOverlay verifies the help overlay opens with '?'.
func TestTeatestHelpOverlay(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "?")

	fm := finalModel(t, tm)

	if !fm.helpVisible {
		t.Error("help overlay should be visible after pressing '?'")
	}
}

// TestTeatestColumnNavigation verifies h/l column navigation.
func TestTeatestColumnNavigation(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "l")

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// First col is id (readonly/PK), so initial cursor starts at col 1 (name).
	// After 'l', should be at col 2 (email).
	if tab.ColCursor != 2 {
		t.Errorf("ColCursor = %d, want 2", tab.ColCursor)
	}
}
