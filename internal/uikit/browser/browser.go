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
// Confirmation for destructive actions is a first-class Action property
// — set Action.Confirm = true and the Model embeds a huh.NewConfirm
// modal (theme + keymap from uikit) on the first press. Yes fires Run;
// No / esc / q just closes the modal without quitting the program.
// Per-entry copy is supplied via Action.ConfirmPrompt. Refreshing after
// a mutation is driven by [RefreshMsg], which the Model translates back
// into a Provider.Children call against the current path.
package browser

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
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
//   - Confirm: true → the first press opens a huh.NewConfirm modal.
//     Yes fires Run; No / esc / q closes the modal without quitting.
//     Default focus is the negative button so an accidental Enter is
//     a no-op.
//   - ConfirmPrompt (optional, paired with Confirm): per-entry modal
//     copy as (title, description). Default if nil is generic — wire
//     this for every destructive action so users see what's about to
//     happen.
//   - Run: returns the tea.Cmd that performs the action. Usually emits
//     a [StatusMsg] for feedback and a [RefreshMsg] for re-fetching the
//     listing after a mutation.
type Action struct {
	Key           key.Binding
	AppliesTo     func(Entry) bool
	Allowed       func(Entry) (bool, string)
	Confirm       bool
	ConfirmPrompt func(Entry) (title, description string)
	Run           func(Entry) tea.Cmd
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
	confirm *confirmState
}

// confirmState owns the embedded huh modal for a single Confirm:true
// action. While non-nil, the parent Update routes all keys through the
// form first — that's how esc/q cancel the modal instead of quitting
// the program.
type confirmState struct {
	form   *huh.Form
	answer bool
	action Action
	entry  Entry
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
//  1. While the confirm modal is active, ALL messages flow to the form
//     so huh's internal NextField/NextGroup cmd cascade reaches it.
//  2. ChildrenMsg / RefreshMsg / StatusMsg — internal protocol.
//  3. tea.WindowSizeMsg — resize list.
//  4. tea.KeyPressMsg — navigation, actions, then list (when not handled).
//  5. Anything else falls through to list.Model.Update.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.confirm != nil {
		return m.routeConfirm(msg)
	}
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
// the list owns everything except quit. Modal routing happens upstream
// in [Model.Update].
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

	// Navigation.
	switch {
	case key.Matches(msg, m.nav.open):
		if e, ok := m.list.SelectedItem().(Entry); ok && e.IsDir() {
			m.cwd = e.Path()
			return m, m.cfg.Provider.Children(m.cwd)
		}
		// Enter on a leaf falls through to action dispatch so a
		// consumer can bind an Action to "enter" (e.g. fspicker's
		// "pick file"). If no action matches, the key is inert.
	case key.Matches(msg, m.nav.up):
		if parent := m.cfg.Provider.Parent(m.cwd); parent != m.cwd {
			m.cwd = parent
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
			return m, nil
		}
		if ok, reason := a.allowed(entry); !ok {
			return m, m.list.NewStatusMessage(renderStatus(StatusMsg{
				Text: reason,
				Kind: StatusWarn,
			}))
		}
		if a.Confirm {
			m.confirm = newConfirmState(a, entry)
			return m, m.confirm.form.Init()
		}
		return m, a.Run(entry)
	}

	// Unrecognized key: defer to list (cursor movement, filter, paging).
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// routeConfirm forwards a message to the active confirm modal and
// inspects the resulting form state. Any msg type is accepted because
// huh emits internal NextField/NextGroup cmds whose resulting msgs
// must reach the form to drive the State transition.
//
// On StateCompleted the answer decides whether Action.Run fires. On
// StateAborted (esc / q / ctrl+c via uikit.HuhKeyMap) the modal closes
// silently — crucially the form's tea.Quit Cmd is discarded so the
// parent program stays alive.
func (m Model) routeConfirm(msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.confirm.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		m.confirm.form = f
	}
	switch m.confirm.form.State {
	case huh.StateCompleted:
		c := m.confirm
		m.confirm = nil
		if c.answer {
			return m, c.action.Run(c.entry)
		}
		return m, nil
	case huh.StateAborted:
		m.confirm = nil
		return m, nil
	}
	return m, cmd
}

// newConfirmState builds the modal for an action+entry pair. Default
// focus lands on the negative button — Enter alone is a no-op so a
// fat-fingered confirm key doesn't compound into an accidental delete.
func newConfirmState(a Action, e Entry) *confirmState {
	title, desc := defaultPrompt(a, e)
	if a.ConfirmPrompt != nil {
		title, desc = a.ConfirmPrompt(e)
	}
	c := &confirmState{action: a, entry: e}
	c.form = huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Description(desc).
			Affirmative("Yes").
			Negative("No").
			Value(&c.answer),
	)).
		WithTheme(uikit.HuhTheme()).
		WithKeyMap(uikit.HuhKeyMap()).
		WithShowHelp(false).
		WithShowErrors(false)
	return c
}

// defaultPrompt is the fallback when an Action does not supply
// ConfirmPrompt. Generic on purpose — consumers should override.
func defaultPrompt(a Action, _ Entry) (string, string) {
	return "Confirm " + a.Key.Help().Key + "?", ""
}

// View renders the browser. While a confirm modal is active the form
// is centered over the list area; the list is hidden because lipgloss
// v2's Place fills the background. A future iteration could composite
// the form on top of a dimmed list via lipgloss.NewLayer.
func (m Model) View() string {
	if m.confirm != nil {
		return lipgloss.Place(
			m.list.Width(),
			m.list.Height(),
			lipgloss.Center,
			lipgloss.Center,
			m.confirm.form.View(),
		)
	}
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
