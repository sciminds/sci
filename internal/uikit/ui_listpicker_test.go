package uikit

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

// ── HardenListKeyMap ──────────────────────────────────────────────────

func TestHardenListKeyMap_FreesNavAndActionKeys(t *testing.T) {
	l := list.New(sampleItems(), list.NewDefaultDelegate(), 0, 0)

	// Sanity: defaults bind l/d/f to NextPage and h/u/b to PrevPage.
	if !key.Matches(tea.KeyPressMsg{Code: 'd'}, l.KeyMap.NextPage) {
		t.Fatal("test premise broken: default keymap should bind d to NextPage")
	}

	HardenListKeyMap(&l)

	// l/h (open/back) and the delegate-action keys d/u/b/f must all be
	// freed from paging now that the shared keymap owns them.
	for _, code := range []rune{'l', 'd', 'f'} {
		if key.Matches(tea.KeyPressMsg{Code: code}, l.KeyMap.NextPage) {
			t.Errorf("HardenListKeyMap left %c bound to NextPage", code)
		}
	}
	for _, code := range []rune{'h', 'u', 'b'} {
		if key.Matches(tea.KeyPressMsg{Code: code}, l.KeyMap.PrevPage) {
			t.Errorf("HardenListKeyMap left %c bound to PrevPage", code)
		}
	}
	// Full-page scrolling lives on PgUp/PgDn.
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyPgDown}, l.KeyMap.NextPage) {
		t.Error("PgDn should page forward after hardening")
	}
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyPgUp}, l.KeyMap.PrevPage) {
		t.Error("PgUp should page backward after hardening")
	}
}

// ── Classify ──────────────────────────────────────────────────────────

func TestClassifyMapsKeysToIntents(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)

	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want Intent
	}{
		{"enter opens", tea.KeyPressMsg{Code: tea.KeyEnter}, IntentOpen},
		{"l opens", tea.KeyPressMsg{Code: 'l'}, IntentOpen},
		{"right opens", tea.KeyPressMsg{Code: tea.KeyRight}, IntentOpen},
		{"esc backs", tea.KeyPressMsg{Code: tea.KeyEscape}, IntentBack},
		{"h backs", tea.KeyPressMsg{Code: 'h'}, IntentBack},
		{"left backs", tea.KeyPressMsg{Code: tea.KeyLeft}, IntentBack},
		{"q quits", tea.KeyPressMsg{Code: 'q'}, IntentQuit},
		{"ctrl+c quits", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, IntentQuit},
		{"j is none", tea.KeyPressMsg{Code: 'j'}, IntentNone},
	}
	for _, tc := range cases {
		if got := lp.Classify(tc.msg); got != tc.want {
			t.Errorf("%s: Classify() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestClassifyHonorsFilterGuard(t *testing.T) {
	lp := NewListPicker("Test", sampleItems())
	lp.SetSize(80, 24)
	// Enter filter mode (the `/` key).
	lp, _ = lp.Update(tea.KeyPressMsg{Code: '/'})
	if !lp.IsFiltering() {
		t.Fatal("expected to be filtering after pressing /")
	}

	// While filtering, nav keys are typed into the filter, not intents.
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'l'}, {Code: 'h'}, {Code: 'q'}, {Code: tea.KeyEnter},
	} {
		if got := lp.Classify(msg); got != IntentNone {
			t.Errorf("Classify(%v) while filtering = %d, want IntentNone", msg, got)
		}
	}
	// ...except ctrl+c, which always quits so a filter can't trap you.
	if got := lp.Classify(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); got != IntentQuit {
		t.Errorf("Classify(ctrl+c) while filtering = %d, want IntentQuit", got)
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
