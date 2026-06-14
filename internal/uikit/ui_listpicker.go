package uikit

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
)

// NewListDelegate returns a list.DefaultDelegate styled to match the TUI theme.
// Used by the help browser and glossary TUIs.
func NewListDelegate() list.DefaultDelegate {
	p := TUI.palette
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(p.Blue).
		BorderLeftForeground(p.Blue)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(p.Blue).
		BorderLeftForeground(p.Blue)
	return d
}

// NewCompactDelegate is [NewListDelegate] with descriptions turned off, so each
// row is a single line. Reach for it when items have no useful second line (or
// fold their detail into the title) and you want a denser list.
func NewCompactDelegate() list.DefaultDelegate {
	d := NewListDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	return d
}

// ── The one keymap (single source of truth) ───────────────────────────
// Every list surface — flat [ListPicker] and hierarchical browser.Model —
// reads these. Defining open/back here is what makes `l` mean the same
// thing everywhere (it used to page in help/learn but open in cloud).

var (
	listOpen = key.NewBinding(
		key.WithKeys("enter", "l", "right"),
		key.WithHelp("enter/l", "open"),
	)
	listBack = key.NewBinding(
		key.WithKeys("esc", "h", "left", "backspace"),
		key.WithHelp("esc/h", "back"),
	)
	// listQuit shows in help and matches q; ctrl+c is handled separately
	// (listQuitHard) so it quits even mid-filter.
	listQuit = key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	)
	listQuitHard = key.NewBinding(key.WithKeys("ctrl+c"))
	listHalfDown = key.NewBinding(key.WithKeys("ctrl+d"))
	listHalfUp   = key.NewBinding(key.WithKeys("ctrl+u"))
)

// HardenListKeyMap reserves the keys the shared keymap owns at the model
// level — l/h (open/back) and d/u/b/f (delegate actions like download) —
// by collapsing the list's own paging bindings down to PgUp/PgDn.
//
// Why: bubbles v2's [list.DefaultKeyMap] binds l/right/d/f to NextPage and
// h/left/u/b to PrevPage, and [list.Model.Update] processes paging *before*
// the delegate runs. Left alone it would swallow `l` as "next page" and run
// a delegate's `d` action on the wrong (already-paged) item. Full-page
// scrolling stays on PgUp/PgDn; half-page is ctrl+d/ctrl+u (see
// [ListCore.Update]).
func HardenListKeyMap(l *list.Model) {
	l.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "next page"),
	)
	l.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "prev page"),
	)
}

// Intent is what a key press means once the filtering guard is applied —
// the "event" a [ListCore] dispatches up so each parent decides the
// behavior (open a sub-list vs. an overlay vs. descend a directory).
//
// In Svelte terms this is the event a child component emits to its parent;
// the parent owns what happens, the child owns only what the key *means*.
type Intent int

const (
	// IntentNone means "not a nav key" — forward it to the list so it
	// drives the cursor, the filter input, or paging.
	IntentNone Intent = iota
	// IntentOpen is enter / l / right: open or descend into the selection.
	IntentOpen
	// IntentBack is esc / h / left: go back / up one level.
	IntentBack
	// IntentQuit is q / ctrl+c: leave the list.
	IntentQuit
)

// ListCore is the shared base every uikit list surface is built from: the
// flat [ListPicker] aliases it directly, and browser.Model embeds it. It
// owns the bubbles list, the one keymap, the help footer, the filtering
// guard, and half-page scrolling — so all callers agree on keys and look
// instead of each re-deriving them.
//
// Svelte mental model (the lens this project reasons in):
//
//	struct fields      → component state    (`let` / `$state`)
//	Update(msg)        → event handlers     (events/props flowing in)
//	View()             → markup / render    (the `$derived` template)
//	tea.Cmd            → a side effect / async action
//	embedding ListCore → nesting a child component
//	Classify → Intent  → an event dispatched up to the parent
type ListCore struct {
	inner list.Model // component state: the wrapped bubbles list
}

// NewListPicker creates a pre-styled filterable list. extraHints are
// surfaced in the footer *after* the standard open/back/quit keys — pass
// action keys here (e.g. cloud's x→delete); navigation help is automatic.
func NewListPicker(title string, items []list.Item, extraHints ...key.Binding) ListCore {
	return newListPicker(title, items, NewListDelegate(), extraHints...)
}

// NewCompactListPicker is [NewListPicker] with single-line rows (see
// [NewCompactDelegate]) — for menus whose items carry no description, or fold
// their detail into the title, e.g. the two-level `sci setup` menu.
func NewCompactListPicker(title string, items []list.Item, extraHints ...key.Binding) ListCore {
	return newListPicker(title, items, NewCompactDelegate(), extraHints...)
}

// newListPicker is the shared builder behind [NewListPicker] and
// [NewCompactListPicker]; only the delegate differs.
func newListPicker(title string, items []list.Item, d list.DefaultDelegate, extraHints ...key.Binding) ListCore {
	l := list.New(items, d, 0, 0)
	HardenListKeyMap(&l)
	l.Title = title
	l.Styles.Title = TUI.TextBlueBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return append([]key.Binding{listOpen, listBack, listQuit}, extraHints...)
	}
	return ListCore{inner: l}
}

// NewListViewer creates a pre-styled filterable list for *non-navigable*
// surfaces — a scroll / filter / quit viewer with nothing to "open" (e.g. the
// brew package list). Its footer advertises only quit plus any extraHints,
// not open/back. Reach for [NewListPicker] when the user descends into rows.
func NewListViewer(title string, items []list.Item, extraHints ...key.Binding) ListCore {
	c := NewListPicker(title, items, extraHints...)
	c.inner.AdditionalShortHelpKeys = func() []key.Binding {
		return append([]key.Binding{listQuit}, extraHints...)
	}
	return c
}

// ListPicker is the flat list surface (no hierarchy, no actions) used by
// sci help, sci learn, and sci setup. It is [ListCore] under another name;
// the alias keeps those call sites reading as "a picker".
type ListPicker = ListCore

// Items converts a typed slice to []list.Item so callers don't need to
// write the lo.Map boilerplate themselves:
//
//	kit.NewListPicker("Books", kit.Items(books), …)
func Items[T list.Item](ts []T) []list.Item {
	return lo.Map(ts, func(t T, _ int) list.Item { return t })
}

// ── Intent classification ─────────────────────────────────────────────

// Classify maps a key press to an [Intent], honoring the filter guard:
// while the user is typing a filter, only ctrl+c is IntentQuit and
// everything else is IntentNone so it reaches the filter input. It is
// read-only — it never consumes the message, so a caller that gets
// IntentNone still forwards the key through [ListCore.Update].
func (c ListCore) Classify(msg tea.KeyPressMsg) Intent {
	// ctrl+c quits unconditionally — even mid-filter — so a wedged filter
	// can always be escaped.
	if key.Matches(msg, listQuitHard) {
		return IntentQuit
	}
	if c.inner.FilterState() == list.Filtering {
		return IntentNone
	}
	switch {
	case key.Matches(msg, listOpen):
		return IntentOpen
	case key.Matches(msg, listBack):
		return IntentBack
	case key.Matches(msg, listQuit):
		return IntentQuit
	}
	return IntentNone
}

// ── Queries ───────────────────────────────────────────────────────────

// Title returns the list title.
func (c ListCore) Title() string { return c.inner.Title }

// IsFiltering returns true when the user is typing a filter query.
// Key handlers should pass q/esc through to the list while filtering.
func (c ListCore) IsFiltering() bool {
	return c.inner.FilterState() == list.Filtering
}

// FilterState returns the underlying list filter state for fine-grained
// checks in tests.
func (c ListCore) FilterState() list.FilterState {
	return c.inner.FilterState()
}

// SelectedItem returns the currently highlighted item, or nil if the
// list is empty. Callers type-assert the result:
//
//	book, ok := lp.SelectedItem().(Book)
func (c ListCore) SelectedItem() list.Item {
	return c.inner.SelectedItem()
}

// Items returns the current item slice (useful for count assertions in tests).
func (c ListCore) Items() []list.Item { return c.inner.Items() }

// Width returns the list's current width.
func (c ListCore) Width() int { return c.inner.Width() }

// Height returns the list's current height.
func (c ListCore) Height() int { return c.inner.Height() }

// ── Mutators ──────────────────────────────────────────────────────────

// SetSize updates the list dimensions.
func (c *ListCore) SetSize(w, h int) { c.inner.SetSize(w, h) }

// SetItems replaces the list contents.
func (c *ListCore) SetItems(items []list.Item) { c.inner.SetItems(items) }

// SetTitle updates the list title (e.g. a browser breadcrumb).
func (c *ListCore) SetTitle(title string) { c.inner.Title = title }

// ResetSelected moves the cursor back to the first item.
func (c *ListCore) ResetSelected() { c.inner.ResetSelected() }

// Select moves the cursor to the item at index.
func (c *ListCore) Select(index int) { c.inner.Select(index) }

// StatusMessage sets a transient status message on the list.
func (c *ListCore) StatusMessage(msg string) { c.inner.NewStatusMessage(msg) }

// NewStatusMessage sets a transient status message and returns the Cmd
// that times it out — for callers that compose it into a tea.Batch.
func (c *ListCore) NewStatusMessage(msg string) tea.Cmd {
	return c.inner.NewStatusMessage(msg)
}

// ── Bubbletea integration ─────────────────────────────────────────────

// Update delegates to the inner list.Model, intercepting half-page
// scrolling (ctrl+d / ctrl+u), which bubbles' list has no binding for.
func (c ListCore) Update(msg tea.Msg) (ListCore, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && c.inner.FilterState() != list.Filtering {
		switch {
		case key.Matches(k, listHalfDown):
			for range max(1, c.inner.Height()/2) {
				c.inner.CursorDown()
			}
			return c, nil
		case key.Matches(k, listHalfUp):
			for range max(1, c.inner.Height()/2) {
				c.inner.CursorUp()
			}
			return c, nil
		}
	}
	var cmd tea.Cmd
	c.inner, cmd = c.inner.Update(msg)
	return c, cmd
}

// View renders the list.
func (c ListCore) View() string { return c.inner.View() }
