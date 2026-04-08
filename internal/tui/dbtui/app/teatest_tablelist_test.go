package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
