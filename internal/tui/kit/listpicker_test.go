package kit

import (
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// testItem implements list.Item for testing.
type testItem struct {
	title string
	desc  string
}

func (t testItem) Title() string       { return t.title }
func (t testItem) Description() string { return t.desc }
func (t testItem) FilterValue() string { return t.title }

func sampleItems() []list.Item {
	return []list.Item{
		testItem{title: "alpha", desc: "first"},
		testItem{title: "beta", desc: "second"},
		testItem{title: "gamma", desc: "third"},
	}
}

// ── Construction ──────────────────────────────────────────────────────

func TestNewListPickerSetsTitle(t *testing.T) {
	lp := NewListPicker("My List", sampleItems())
	if got := lp.Title(); got != "My List" {
		t.Errorf("Title() = %q, want %q", got, "My List")
	}
}

func TestNewListPickerWithHints(t *testing.T) {
	hints := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
	lp := NewListPicker("Test", sampleItems(), hints...)
	// Should not panic; title set correctly.
	if got := lp.Title(); got != "Test" {
		t.Errorf("Title() = %q, want %q", got, "Test")
	}
}

func TestNewListPickerEmpty(t *testing.T) {
	lp := NewListPicker("Empty", nil)
	if got := lp.Title(); got != "Empty" {
		t.Errorf("Title() = %q, want %q", got, "Empty")
	}
}

// ── IsFiltering ───────────────────────────────────────────────────────

func TestListPickerIsFilteringDefault(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	if lp.IsFiltering() {
		t.Error("IsFiltering() should be false by default")
	}
}

// ── SelectedItem ──────────────────────────────────────────────────────

func TestListPickerSelectedItem(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)
	item := lp.SelectedItem()
	if item == nil {
		t.Fatal("SelectedItem() returned nil")
	}
	ti, ok := item.(testItem)
	if !ok {
		t.Fatalf("SelectedItem() returned %T, want testItem", item)
	}
	if ti.title != "alpha" {
		t.Errorf("selected title = %q, want %q", ti.title, "alpha")
	}
}

// ── SetSize ───────────────────────────────────────────────────────────

func TestListPickerSetSize(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(120, 40) // should not panic
}

// ── StatusMessage ─────────────────────────────────────────────────────

func TestListPickerStatusMessage(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)
	lp.StatusMessage("hello") // should not panic
}

// ── Update ────────────────────────────────────────────────────────────

func TestListPickerUpdateReturnsListPicker(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)
	lp2, cmd := lp.Update(tea.KeyPressMsg{Code: 'j'})
	// lp2 should be a ListPicker (compile-time check).
	_ = lp2
	_ = cmd
}

// ── View ──────────────────────────────────────────────────────────────

func TestListPickerView(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)
	v := lp.View()
	if v == "" {
		t.Error("View() should not be empty after SetSize")
	}
}

// ── Items helper ──────────────────────────────────────────────────────

func TestItemsConvertsSlice(t *testing.T) {
	src := []testItem{
		{title: "a"},
		{title: "b"},
		{title: "c"},
	}
	items := Items(src)
	if len(items) != 3 {
		t.Fatalf("Items() returned %d items, want 3", len(items))
	}
	for i, item := range items {
		ti, ok := item.(testItem)
		if !ok {
			t.Fatalf("items[%d] is %T, want testItem", i, item)
		}
		if ti.title != src[i].title {
			t.Errorf("items[%d].title = %q, want %q", i, ti.title, src[i].title)
		}
	}
}
