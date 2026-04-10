package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestTableListNavigate verifies j/k moves the cursor in the table list.
func TestTeatestTableListNavigate(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "t") // open table list
	sendKey(tm, "j") // move down

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Fatal("table list should be open")
	}
	if fm.tableList.Cursor != 1 {
		t.Errorf("tableList.Cursor = %d, want 1", fm.tableList.Cursor)
	}
}

// TestTeatestTableListSwitch verifies Enter switches to the selected table.
func TestTeatestTableListSwitch(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "t") // open table list
	sendKey(tm, "j") // move to second entry (users)
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)

	if fm.tableList != nil {
		t.Error("table list should be closed after Enter")
	}
	// Should have switched to the "users" tab (index 1).
	if fm.active != 1 {
		t.Errorf("active tab = %d, want 1", fm.active)
	}
}

// TestTeatestTableListRename verifies renaming a table through the overlay.
func TestTeatestTableListRename(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t")
	sendKey(tm, "r") // start rename
	// Clear and type new name.
	tm.Send(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tm.Send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	tm.Type("items")
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)
	_ = fm

	// Verify DB table was renamed.
	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	found := false
	for _, n := range names {
		if n == "items" {
			found = true
		}
		if n == "products" {
			t.Error("old table name 'products' still exists")
		}
	}
	if !found {
		t.Error("new table name 'items' not found")
	}
}

// TestTeatestTableListCreate verifies creating a new empty table.
func TestTeatestTableListCreate(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t")
	sendKey(tm, "c") // start create
	tm.Type("orders")
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)
	_ = fm

	// Verify new table exists.
	count, err := store.TableRowCount("orders")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows in new table, got %d", count)
	}
}

// TestTeatestTableListDelete verifies deleting a table.
func TestTeatestTableListDelete(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t")
	// Cursor starts on "products" (first entry). Delete it.
	sendKey(tm, "d")

	fm := finalModel(t, tm)
	_ = fm

	// Verify table was dropped.
	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	for _, n := range names {
		if n == "products" {
			t.Error("table 'products' should have been deleted")
		}
	}
}

// TestTeatestTableListClose verifies Esc closes the table list.
func TestTeatestTableListClose(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "t")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.tableList != nil {
		t.Error("table list should be closed after Esc")
	}
}

// ── Derive table/view ──────────────────────────────────────────────────

// TestTeatestTableListDeriveTable verifies creating a derived table via SQL.
func TestTeatestTableListDeriveTable(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t") // open table list
	sendKey(tm, "s") // start derive

	// The SQL textarea is pre-filled with "SELECT ". Type the rest.
	tm.Type("title, price FROM products WHERE price > 5")

	// Tab to name field.
	sendSpecial(tm, tea.KeyTab)
	// Clear default name "derived" and type new name.
	tm.Send(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tm.Send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	tm.Type("expensive")

	// Enter = create table (not view).
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)
	_ = fm

	// Verify the derived table exists with correct data.
	count, err := store.TableRowCount("expensive")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	// products with price > 5: Widget (9.99), Gadget (24.50) = 2 rows.
	if count != 2 {
		t.Errorf("derived table row count = %d, want 2", count)
	}
}

// TestTeatestTableListDeriveCancel verifies Esc cancels the derive overlay.
func TestTeatestTableListDeriveCancel(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t")
	sendKey(tm, "s")               // start derive
	sendSpecial(tm, tea.KeyEscape) // cancel

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Error("table list should still be open after derive cancel")
	}
	if fm.tableList != nil && fm.tableList.Deriving {
		t.Error("Deriving should be false after Esc")
	}

	// Verify no "derived" table was created.
	names, err := store.TableNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "derived" {
			t.Error("derived table should not exist after cancel")
		}
	}
}

// ── Export ─────────────────────────────────────────────────────────────

// TestTeatestTableListExport verifies exporting a table to CSV.
func TestTeatestTableListExport(t *testing.T) {
	// Run in a temp dir so the export file lands there.
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	tm, _ := startTeatest(t)

	sendKey(tm, "t") // open table list (cursor on "products")
	sendKey(tm, "e") // export

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Fatal("table list should still be open after export")
	}
	if !strings.Contains(fm.tableList.Status, "Exported") {
		t.Errorf("expected export status, got %q", fm.tableList.Status)
	}

	// Verify CSV file was created.
	csvData, err := os.ReadFile(filepath.Join(tmp, "products.csv"))
	if err != nil {
		t.Fatalf("exported CSV file not found: %v", err)
	}
	if !strings.Contains(string(csvData), "Widget") {
		t.Error("exported CSV should contain 'Widget'")
	}
}

// ── Dedup ──────────────────────────────────────────────────────────────

// TestTeatestTableListDedup verifies dedup removes duplicate rows.
func TestTeatestTableListDedup(t *testing.T) {
	// Create a table without a PK so rows can be truly duplicate (all columns match).
	stmts := []string{
		`CREATE TABLE items (name TEXT)`,
		`INSERT INTO items (name) VALUES ('apple'), ('banana'), ('apple'), ('apple')`,
	}
	m, store := newTeatestModelWithSchema(t, stmts)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForOutput(t, tm, "items")

	sendKey(tm, "t") // open table list
	sendKey(tm, "u") // dedup

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Fatal("table list should still be open")
	}
	if !strings.Contains(fm.tableList.Status, "duplicate") {
		t.Errorf("expected dedup status, got %q", fm.tableList.Status)
	}

	// Verify duplicates were removed: 2 unique values remain.
	count, err := store.TableRowCount("items")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count after dedup = %d, want 2 (apple + banana)", count)
	}
}

// ── Derive as view ────────────────────────────────────────────────────

// TestTeatestTableListDeriveView verifies creating a derived view via Shift+Enter.
func TestTeatestTableListDeriveView(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t") // open table list
	sendKey(tm, "s") // start derive

	tm.Type("title FROM products")

	// Tab to name field, clear, type name.
	sendSpecial(tm, tea.KeyTab)
	tm.Send(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tm.Send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	tm.Type("product_names")

	// Shift+Enter = create view (not table).
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})

	fm := finalModel(t, tm)
	_ = fm

	// Verify the view exists and is queryable.
	count, err := store.TableRowCount("product_names")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 3 {
		t.Errorf("view row count = %d, want 3", count)
	}
}

// ── Create validation ─────────────────────────────────────────────────

// TestTeatestTableListCreateInvalidName verifies invalid names are rejected.
func TestTeatestTableListCreateInvalidName(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "t")
	sendKey(tm, "c") // start create
	tm.Type("no;sql-injection")
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Fatal("table list should still be open")
	}
	if !strings.Contains(fm.tableList.Status, "Invalid") {
		t.Errorf("expected invalid name error, got %q", fm.tableList.Status)
	}

	// Verify table was NOT created.
	names, err := store.TableNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "no;sql-injection" {
			t.Error("invalid table name should not have been created")
		}
	}
}

// TestTeatestTableListCreateDuplicate verifies duplicate names are rejected.
func TestTeatestTableListCreateDuplicate(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "t")
	sendKey(tm, "c")    // start create
	tm.Type("products") // already exists
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Fatal("table list should still be open")
	}
	if !strings.Contains(fm.tableList.Status, "already exists") {
		t.Errorf("expected duplicate error, got %q", fm.tableList.Status)
	}
}

// ── Rapid tab switching ────────────────────────────────────────────────

// TestTeatestRapidTabSwitch verifies rapid tab switching doesn't panic.
func TestTeatestRapidTabSwitch(t *testing.T) {
	tm, _ := startTeatest(t)

	// Rapidly switch tabs multiple times before any async load completes.
	sendSpecial(tm, tea.KeyTab)
	sendSpecial(tm, tea.KeyTab)
	sendSpecial(tm, tea.KeyTab)
	sendSpecial(tm, tea.KeyTab)
	tm.Send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})

	fm := finalModel(t, tm)

	// Just verify we didn't panic and the model is in a consistent state.
	if fm.active < 0 || fm.active >= len(fm.tabs) {
		t.Errorf("active tab %d is out of range [0, %d)", fm.active, len(fm.tabs))
	}
}
