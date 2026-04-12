package ui

// selectlist.go — generic multi-select list component used by TUI wizards
// (e.g. sci new, sci doctor setup). Items implement [SelectItem].

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
)

// SelectItem is the interface that items in a SelectList must implement.
type SelectItem interface {
	// SelectTitle returns the primary display text for the item.
	SelectTitle() string
}

// SelectList is a reusable Bubble Tea model for a toggle-select list.
// It handles cursor movement, space to toggle, 'a' to toggle all, and
// enter to confirm. The parent TUI embeds this model and forwards
// Update() calls.
type SelectList struct {
	items     []selectEntry
	cursor    int
	width     int
	heading   string
	confirmed bool

	// RenderItem is an optional callback for custom item rendering.
	// It receives the item, whether it's selected, whether the cursor
	// is on it, and returns the rendered string for that item.
	// If nil, a default renderer is used.
	RenderItem func(item SelectItem, selected, isCursor bool) string
}

type selectEntry struct {
	item     SelectItem
	selected bool
}

// SelectListOption configures a SelectList.
type SelectListOption func(*SelectList)

// WithHeading sets the heading displayed above the list.
func WithHeading(h string) SelectListOption {
	return func(sl *SelectList) { sl.heading = h }
}

// WithSelected sets the initial selection state for each item by index.
func WithSelected(selected []bool) SelectListOption {
	return func(sl *SelectList) {
		for i := range sl.items {
			if i < len(selected) {
				sl.items[i].selected = selected[i]
			}
		}
	}
}

// WithRenderItem sets a custom item renderer.
func WithRenderItem(fn func(item SelectItem, selected, isCursor bool) string) SelectListOption {
	return func(sl *SelectList) { sl.RenderItem = fn }
}

// NewSelectList creates a new SelectList with the given items.
func NewSelectList(items []SelectItem, opts ...SelectListOption) SelectList {
	entries := lo.Map(items, func(item SelectItem, _ int) selectEntry {
		return selectEntry{item: item}
	})
	sl := SelectList{items: entries}
	for _, opt := range opts {
		opt(&sl)
	}
	return sl
}

// ── Queries ─────────────────────────────────────────────────────────────────

// SelectedIndices returns the indices of selected items.
func (sl SelectList) SelectedIndices() []int {
	var indices []int
	for i, e := range sl.items {
		if e.selected {
			indices = append(indices, i)
		}
	}
	return indices
}

// SelectedCount returns how many items are selected.
func (sl SelectList) SelectedCount() int {
	return lo.CountBy(sl.items, func(e selectEntry) bool {
		return e.selected
	})
}

// IsConfirmed returns true after the user pressed enter with at least one selection.
func (sl SelectList) IsConfirmed() bool { return sl.confirmed }

// IsSelected returns whether the item at index i is selected.
func (sl SelectList) IsSelected(i int) bool {
	if i < 0 || i >= len(sl.items) {
		return false
	}
	return sl.items[i].selected
}

// SetSelected sets the selection state for the item at index i.
func (sl *SelectList) SetSelected(i int, v bool) {
	if i >= 0 && i < len(sl.items) {
		sl.items[i].selected = v
	}
}

// Len returns the number of items.
func (sl SelectList) Len() int { return len(sl.items) }

// Item returns the SelectItem at index i.
func (sl SelectList) Item(i int) SelectItem {
	if i < 0 || i >= len(sl.items) {
		return nil
	}
	return sl.items[i].item
}

// SetWidth updates the width available for rendering.
func (sl *SelectList) SetWidth(w int) { sl.width = w }

// ── Key map ─────────────────────────────────────────────────────────────────

// SelectListKeys is the help.KeyMap for the selecting phase.
type SelectListKeys struct {
	Toggle key.Binding
	All    key.Binding
}

// NewSelectListKeys returns the default key map for a select list.
func NewSelectListKeys() SelectListKeys {
	return SelectListKeys{
		Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
	}
}

func (k SelectListKeys) ShortHelp() []key.Binding {
	return []key.Binding{BindUp, BindDown, k.Toggle, k.All, BindEnter, BindQuit}
}
func (k SelectListKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ── Update ──────────────────────────────────────────────────────────────────

// Update handles key messages for the select list. Returns the updated model
// and a tea.Cmd. When the user presses enter with at least one selection,
// IsConfirmed() becomes true. When enter is pressed with nothing selected,
// it returns tea.Quit.
func (sl SelectList) Update(msg tea.Msg) (SelectList, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return sl, nil
	}

	switch keyMsg.String() {
	case KeyUp, KeyK:
		if sl.cursor > 0 {
			sl.cursor--
		}
	case KeyDown, KeyJ:
		if sl.cursor < len(sl.items)-1 {
			sl.cursor++
		}
	case KeySpace:
		if sl.cursor >= 0 && sl.cursor < len(sl.items) {
			sl.items[sl.cursor].selected = !sl.items[sl.cursor].selected
		}
	case KeyA:
		allSelected := true
		for _, e := range sl.items {
			if !e.selected {
				allSelected = false
				break
			}
		}
		for i := range sl.items {
			sl.items[i].selected = !allSelected
		}
	case KeyEnter:
		anySelected := false
		for _, e := range sl.items {
			if e.selected {
				anySelected = true
				break
			}
		}
		if !anySelected {
			return sl, tea.Quit
		}
		sl.confirmed = true
	}

	return sl, nil
}

// ── View ────────────────────────────────────────────────────────────────────

// View renders the select list.
func (sl SelectList) View() string {
	var lines []string

	if sl.heading != "" {
		lines = append(lines, TUI.Heading().Render(sl.heading))
		lines = append(lines, "")
	}

	for i, e := range sl.items {
		isCursor := i == sl.cursor

		if sl.RenderItem != nil {
			lines = append(lines, sl.RenderItem(e.item, e.selected, isCursor))
		} else {
			lines = append(lines, sl.defaultRender(e, isCursor))
		}
	}

	return strings.Join(lines, "\n")
}

// RenderSelectItemLine renders the cursor/marker/name skeleton common to all
// select list items. Callers append their own suffix or extra lines.
func RenderSelectItemLine(name string, selected, isCursor bool) string {
	cursor := "  "
	if isCursor {
		cursor = TUI.Accent().Render(" " + IconCursor)
	}

	marker := TUI.Muted().Render(IconPending)
	if selected {
		marker = TUI.Pass().Render(IconDot)
	}

	if isCursor {
		name = TUI.Cursor().Render(" " + name + " ")
	}

	return fmt.Sprintf("%s %s %s", cursor, marker, name)
}

func (sl SelectList) defaultRender(e selectEntry, isCursor bool) string {
	return RenderSelectItemLine(e.item.SelectTitle(), e.selected, isCursor)
}

// ── Help integration ────────────────────────────────────────────────────────

// HelpView returns the help bar for the select list using the given help.Model.
func (sl SelectList) HelpView(h help.Model) string {
	return h.View(NewSelectListKeys())
}
