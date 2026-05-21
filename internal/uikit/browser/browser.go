// Package browser is a uikit primitive: a hierarchical list browser
// driven by a Provider (for fetching children of a path) and a list of
// Actions (key-bound operations on the highlighted entry).
//
// The same Model backs three target consumers: the cloud-bucket browser
// (flat snapshot, hierarchy derived from key prefixes), a local
// filesystem picker (os.ReadDir per directory), and the Hugging Face
// org repo browser (paginated remote API). Each consumer wires up its
// own Provider + Actions; the Model owns navigation, breadcrumb, filter
// mode, and the standard list look.
//
// Two-press confirmation for destructive actions is a first-class
// Action property — set Action.Confirm = true and the Model handles the
// "press X again to confirm" UX on its own. Refreshing after a mutation
// is driven by [RefreshMsg], which the Model translates back into a
// Provider.Children call against the current path.
package browser

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// Entry is one row in the browser. Implementations decide what
// metadata to surface in the description column.
type Entry interface {
	list.Item

	// Path is the full, provider-specific path identifying this entry.
	// Passed to [Provider.Children] when the user descends into a
	// directory and to [Provider.Parent] when they ascend.
	Path() string
	// IsDir reports whether the entry is navigable (Enter descends).
	IsDir() bool
}

// Provider supplies the tree on demand. Implementations vary widely:
// cloud derives children from an in-memory snapshot, filesystem calls
// os.ReadDir, HF makes a paginated API call. The Model never inspects
// the path string itself — it just round-trips it.
type Provider interface {
	// Children returns a tea.Cmd that emits a [ChildrenMsg] with the
	// entries directly under path. Errors should be surfaced in the
	// message's Err field; the Model will display them as a status
	// toast and leave the previous listing in place.
	Children(path string) tea.Cmd
	// Root returns the initial path (typically "").
	Root() string
	// Parent returns the parent of path, or the same value when path
	// is already the root.
	Parent(path string) string
	// Breadcrumb returns the display title for the given path. Called
	// every time the listing rebuilds; usually a single line like
	// "sciminds/private / ejolly / data".
	Breadcrumb(path string) string
}

// ChildrenMsg is emitted by [Provider.Children] when results arrive.
// On error, Entries is ignored and Err is surfaced as a status toast.
type ChildrenMsg struct {
	Path    string
	Entries []Entry
	Err     error
}

// RefreshMsg asks the Model to re-run Provider.Children for the current
// path. Mutating actions (delete, rename) emit it from their Cmd so the
// listing reflects the new state without the caller having to re-wire
// data flow.
type RefreshMsg struct{}

// StatusKind selects the visual treatment for a [StatusMsg].
type StatusKind int

const (
	// StatusInfo: neutral progress text ("Deleting foo…").
	StatusInfo StatusKind = iota
	// StatusSuccess: green pass styling ("Deleted foo").
	StatusSuccess
	// StatusWarn: yellow warning ("cannot delete folders").
	StatusWarn
	// StatusError: red failure ("Delete failed: timeout").
	StatusError
)

// StatusMsg renders text in the list's status bar with the given kind.
// Actions emit these freely; the Model just routes them to
// list.NewStatusMessage with the right uikit style.
type StatusMsg struct {
	Text string
	Kind StatusKind
}

// Action binds a key to an operation on the highlighted entry.
//
//   - AppliesTo (optional): false → the key is ignored and the binding
//     is hidden from help. Used for "delete is a file-only action".
//   - Allowed (optional): false + reason → reason is shown as a warn
//     toast and Run does not fire. Used for ownership/permission rules.
//   - Confirm: true → the first press shows a "press again to confirm"
//     toast; the second press in a row (no other key in between) calls
//     Run. Any other key cancels.
//   - Run: returns the tea.Cmd that performs the action. Usually emits
//     a [StatusMsg] for feedback and a [RefreshMsg] for re-fetching the
//     listing after a mutation.
type Action struct {
	Key       key.Binding
	AppliesTo func(Entry) bool
	Allowed   func(Entry) (bool, string)
	Confirm   bool
	Run       func(Entry) tea.Cmd
}

// applies reports whether this action's key applies to the given entry.
// nil AppliesTo means "applies to every entry".
func (a Action) applies(e Entry) bool {
	if a.AppliesTo == nil {
		return true
	}
	return a.AppliesTo(e)
}

// allowed reports whether the action may run on the entry. nil Allowed
// means "always allowed".
func (a Action) allowed(e Entry) (bool, string) {
	if a.Allowed == nil {
		return true, ""
	}
	return a.Allowed(e)
}

// Config configures a [Model]. Title is the initial breadcrumb shown
// before the first ChildrenMsg arrives; once data lands the Model uses
// Provider.Breadcrumb.
type Config struct {
	Title    string
	Provider Provider
	Actions  []Action
	// QuitKeys, when non-empty, are returned as tea.Quit by the Model.
	// Set to nil when embedding inside a larger TUI that owns its own
	// quit handling.
	QuitKeys key.Binding
}

// navKeys are the built-in navigation bindings. Not configurable in v1.
type navKeys struct {
	open key.Binding
	up   key.Binding
}

func newNavKeys() navKeys {
	return navKeys{
		open: key.NewBinding(
			key.WithKeys("enter", "right", "l"),
			key.WithHelp("enter", "open"),
		),
		up: key.NewBinding(
			key.WithKeys("backspace", "left", "h"),
			key.WithHelp("⌫", "up"),
		),
	}
}

// Model is the browser. It implements [tea.Model]. Callers either embed
// it inside a larger TUI and route Update msgs through it, or run it
// standalone via [Run].
type Model struct {
	cfg     Config
	nav     navKeys
	list    list.Model
	cwd     string
	pending *pendingAction
}

// pendingAction tracks a Confirm-style action awaiting its second press.
// Path scopes the confirmation to a specific entry — if the user moves
// the cursor, the pending state is cleared.
type pendingAction struct {
	keyHelp string // the key string ("x", "d", …) to match next press
	path    string // entry the action was primed against
}

// New constructs a [Model]. It does not fetch children yet — call
// [Model.Init] (or rely on the bubbletea program to do so) to kick off
// the initial Provider.Children fetch.
func New(cfg Config) Model {
	delegate := uikit.NewListDelegate()
	l := list.New(nil, delegate, 0, 0)
	uikit.HardenListKeyMap(&l)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Title = cfg.Title
	l.Styles.Title = uikit.TUI.TextBlueBold()

	// Surface action keys + quit in the list's short help.
	nav := newNavKeys()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		help := []key.Binding{nav.open, nav.up}
		for _, a := range cfg.Actions {
			help = append(help, a.Key)
		}
		if cfg.QuitKeys.Keys() != nil {
			help = append(help, cfg.QuitKeys)
		}
		return help
	}

	return Model{
		cfg:  cfg,
		nav:  nav,
		list: l,
		cwd:  cfg.Provider.Root(),
	}
}

// Init returns the initial Cmd: fetch children for the root path.
func (m Model) Init() tea.Cmd {
	return m.cfg.Provider.Children(m.cwd)
}

// Update routes incoming messages. Order matters:
//  1. ChildrenMsg / RefreshMsg / StatusMsg — internal protocol.
//  2. tea.WindowSizeMsg — resize list.
//  3. tea.KeyPressMsg — navigation, actions, then list (when not handled).
//  4. Anything else falls through to list.Model.Update.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ChildrenMsg:
		return m.handleChildren(msg)
	case RefreshMsg:
		return m, m.cfg.Provider.Children(m.cwd)
	case StatusMsg:
		return m, m.list.NewStatusMessage(renderStatus(msg))
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// handleChildren replaces the list contents with the new entries.
// Errors are surfaced as a warn toast; the previous listing stays.
func (m Model) handleChildren(msg ChildrenMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		return m, m.list.NewStatusMessage(renderStatus(StatusMsg{
			Text: msg.Err.Error(),
			Kind: StatusError,
		}))
	}
	items := lo.Map(msg.Entries, func(e Entry, _ int) list.Item { return e })
	m.list.SetItems(items)
	m.list.Title = m.cfg.Provider.Breadcrumb(msg.Path)
	m.list.ResetSelected()
	return m, nil
}

// handleKey dispatches a key press. While the filter input is active,
// the list owns everything except quit.
func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	// Quit (only when configured — embedded models leave it to parent).
	if m.cfg.QuitKeys.Keys() != nil && key.Matches(msg, m.cfg.QuitKeys) {
		return m, tea.Quit
	}

	// Navigation. Both Enter (on dir) and Backspace clear any pending
	// confirmation — the user has moved on.
	switch {
	case key.Matches(msg, m.nav.open):
		if e, ok := m.list.SelectedItem().(Entry); ok && e.IsDir() {
			m.cwd = e.Path()
			m.pending = nil
			return m, m.cfg.Provider.Children(m.cwd)
		}
		// Enter on a leaf is intentionally inert — actions like "d"
		// drive downloads.
		return m, nil
	case key.Matches(msg, m.nav.up):
		if parent := m.cfg.Provider.Parent(m.cwd); parent != m.cwd {
			m.cwd = parent
			m.pending = nil
			return m, m.cfg.Provider.Children(m.cwd)
		}
		return m, nil
	}

	// Action dispatch.
	entry, hasEntry := m.list.SelectedItem().(Entry)
	for _, a := range m.cfg.Actions {
		if !key.Matches(msg, a.Key) {
			continue
		}
		if !hasEntry {
			return m, nil
		}
		if !a.applies(entry) {
			m.pending = nil
			return m, nil
		}
		if ok, reason := a.allowed(entry); !ok {
			m.pending = nil
			return m, m.list.NewStatusMessage(renderStatus(StatusMsg{
				Text: reason,
				Kind: StatusWarn,
			}))
		}
		if a.Confirm {
			actionKey := strings.Join(a.Key.Keys(), ",")
			if m.pending != nil && m.pending.keyHelp == actionKey && m.pending.path == entry.Path() {
				m.pending = nil
				return m, a.Run(entry)
			}
			m.pending = &pendingAction{keyHelp: actionKey, path: entry.Path()}
			return m, m.list.NewStatusMessage(renderStatus(StatusMsg{
				Text: "Press " + a.Key.Help().Key + " again to confirm",
				Kind: StatusWarn,
			}))
		}
		m.pending = nil
		return m, a.Run(entry)
	}

	// Unrecognized key: clear pending, defer to list (cursor movement,
	// filter activation, paging).
	m.pending = nil
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the browser.
func (m Model) View() string {
	return m.list.View()
}

// Path returns the current working path.
func (m Model) Path() string { return m.cwd }

// Selected returns the entry under the cursor, or (nil, false) when
// the list is empty.
func (m Model) Selected() (Entry, bool) {
	e, ok := m.list.SelectedItem().(Entry)
	return e, ok
}

// renderStatus applies the uikit palette to a StatusMsg.
func renderStatus(s StatusMsg) string {
	switch s.Kind {
	case StatusSuccess:
		return uikit.TUI.Pass().Render(s.Text)
	case StatusWarn:
		return uikit.TUI.Warn().Render(s.Text)
	case StatusError:
		return uikit.TUI.Fail().Render(s.Text)
	default: // StatusInfo
		return uikit.TUI.Dim().Render(s.Text)
	}
}

// SendMsg is a convenience helper for Actions: returns a tea.Cmd that
// dispatches the given message immediately. Useful when composing with
// tea.Batch / tea.Sequence inside an Action.Run body.
func SendMsg(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
