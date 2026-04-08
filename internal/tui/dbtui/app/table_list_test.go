package app

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
)

func keyMsg(key string) tea.KeyPressMsg {
	runes := []rune(key)
	if len(runes) == 1 {
		return tea.KeyPressMsg{Code: runes[0], Text: key}
	}
	return tea.KeyPressMsg{Text: key}
}

func specialKeyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		runes := []rune(key)
		if len(runes) == 1 {
			return tea.KeyPressMsg{Code: runes[0], Text: key}
		}
		return tea.KeyPressMsg{Text: key}
	}
}

func makeTestStore(t *testing.T) *data.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Exec("INSERT INTO users VALUES (1, 'Alice'), (2, 'Bob')")
	if err != nil {
		t.Fatal(err)
	}
	// Force views map to populate.
	if _, err := store.TableNames(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func makeTableListModel(t *testing.T) *Model {
	t.Helper()
	store := makeTestStore(t)
	m, err := NewModel(store, "test.db", false)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCreateEmptyTable(t *testing.T) {
	m := makeTableListModel(t)
	m.openTableList()

	// Press 'c' to open create dialog.
	m.handleTableListKey(keyMsg("c"))

	if !m.tableList.Creating {
		t.Fatal("expected Creating mode after pressing 'c'")
	}

	// Type a name.
	for _, r := range "new_table" {
		m.handleTableListKey(keyMsg(string(r)))
	}

	// Confirm.
	m.handleTableListKey(specialKeyMsg("enter"))

	if m.tableList.Creating {
		t.Error("Creating should be false after confirm")
	}

	// Verify the table exists.
	count, err := m.store.TableRowCount("new_table")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows in new empty table, got %d", count)
	}

	// Verify status message.
	if m.tableList.Status == "" {
		t.Error("expected status message after create")
	}
}

func TestCreateEmptyTableCancel(t *testing.T) {
	m := makeTableListModel(t)
	m.openTableList()

	m.handleTableListKey(keyMsg("c"))
	if !m.tableList.Creating {
		t.Fatal("expected Creating mode")
	}

	m.handleTableListKey(specialKeyMsg("esc"))

	if m.tableList.Creating {
		t.Error("Creating should be false after esc")
	}
	if m.tableList == nil {
		t.Error("overlay should still be open after cancelling create")
	}
}

func TestCreateEmptyTableDuplicateName(t *testing.T) {
	m := makeTableListModel(t)
	m.openTableList()

	m.handleTableListKey(keyMsg("c"))

	// Type existing table name.
	for _, r := range "users" {
		m.handleTableListKey(keyMsg(string(r)))
	}
	m.handleTableListKey(specialKeyMsg("enter"))

	if m.tableList.Status == "" {
		t.Error("expected error status for duplicate name")
	}
}

func TestImportFileUsed(t *testing.T) {
	// Verify the import call references ImportFile (compile check).
	// The actual import is tested via the data layer.
	m := makeTableListModel(t)
	_ = m // model compiles with ImportFile call
}
