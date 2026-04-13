package kit

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// ListPicker wraps [list.Model] with the standard project styling:
// filtered search, status bar, accent-styled title, and custom hint
// keys. It eliminates the repeated 10-line constructor + filtering
// guard that appears in every TUI with a filterable list.
type ListPicker struct {
	inner list.Model
}

// NewListPicker creates a pre-styled filterable list. The hints (if
// any) are shown as additional short help keys (e.g. enter→open,
// esc→back).
func NewListPicker(title string, items []list.Item, hints ...key.Binding) ListPicker {
	d := ui.NewListDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = title
	l.Styles.Title = ui.TUI.TextBlueBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	if len(hints) > 0 {
		l.AdditionalShortHelpKeys = func() []key.Binding { return hints }
	}
	return ListPicker{inner: l}
}

// Items converts a typed slice to []list.Item so callers don't need to
// write the lo.Map boilerplate themselves:
//
//	kit.NewListPicker("Books", kit.Items(books), …)
func Items[T list.Item](ts []T) []list.Item {
	return lo.Map(ts, func(t T, _ int) list.Item { return t })
}

// ── Queries ───────────────────────────────────────────────────────────

// Title returns the list title.
func (lp ListPicker) Title() string { return lp.inner.Title }

// IsFiltering returns true when the user is typing a filter query.
// Key handlers should pass q/esc through to the list while filtering.
func (lp ListPicker) IsFiltering() bool {
	return lp.inner.FilterState() == list.Filtering
}

// FilterState returns the underlying list filter state for fine-grained
// checks in tests.
func (lp ListPicker) FilterState() list.FilterState {
	return lp.inner.FilterState()
}

// SelectedItem returns the currently highlighted item, or nil if the
// list is empty. Callers type-assert the result:
//
//	book, ok := lp.SelectedItem().(Book)
func (lp ListPicker) SelectedItem() list.Item {
	return lp.inner.SelectedItem()
}

// Items returns the current item slice (useful for count assertions in tests).
func (lp ListPicker) Items() []list.Item { return lp.inner.Items() }

// ── Mutators ──────────────────────────────────────────────────────────

// SetSize updates the list dimensions.
func (lp *ListPicker) SetSize(w, h int) { lp.inner.SetSize(w, h) }

// StatusMessage sets a transient status message on the list.
func (lp *ListPicker) StatusMessage(msg string) { lp.inner.NewStatusMessage(msg) }

// ── Bubbletea integration ─────────────────────────────────────────────

// Update delegates to the inner list.Model.
func (lp ListPicker) Update(msg tea.Msg) (ListPicker, tea.Cmd) {
	var cmd tea.Cmd
	lp.inner, cmd = lp.inner.Update(msg)
	return lp, cmd
}

// View renders the list.
func (lp ListPicker) View() string { return lp.inner.View() }
