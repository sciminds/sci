package browser

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// ── Test fixtures ───────────────────────────────────────────────────────────

// stubEntry is a minimal Entry suitable for Update-loop tests. It does
// not bother with list.Item.FilterValue niceties beyond returning Name.
type stubEntry struct {
	name  string
	path  string
	isDir bool
}

func (e stubEntry) Title() string       { return e.name }
func (e stubEntry) Description() string { return "" }
func (e stubEntry) FilterValue() string { return e.name }
func (e stubEntry) Path() string        { return e.path }
func (e stubEntry) IsDir() bool         { return e.isDir }

// stubProvider returns children pulled from an in-memory map keyed by
// path. Test cases pre-populate the map, then assert on the messages
// the Model emits.
type stubProvider struct {
	tree          map[string][]Entry // path → children
	root          string
	childrenCalls []string // every path Children() was invoked with
}

func newStubProvider() *stubProvider {
	return &stubProvider{
		tree: map[string][]Entry{
			"": {
				stubEntry{name: "alice", path: "alice", isDir: true},
				stubEntry{name: "ejolly", path: "ejolly", isDir: true},
			},
			"alice": {
				stubEntry{name: "results.csv", path: "alice/results.csv"},
			},
			"ejolly": {
				stubEntry{name: "data", path: "ejolly/data", isDir: true},
				stubEntry{name: "report.pdf", path: "ejolly/report.pdf"},
			},
		},
	}
}

func (p *stubProvider) Children(path string) tea.Cmd {
	p.childrenCalls = append(p.childrenCalls, path)
	entries := p.tree[path]
	return func() tea.Msg {
		return ChildrenMsg{Path: path, Entries: entries}
	}
}
func (p *stubProvider) Root() string { return p.root }
func (p *stubProvider) Parent(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx]
	}
	return ""
}
func (p *stubProvider) Breadcrumb(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

// runCmd invokes a tea.Cmd and returns its message. Returns nil for nil
// Cmds (no message to dispatch).
func runCmd(c tea.Cmd) tea.Msg {
	if c == nil {
		return nil
	}
	return c()
}

// keyPress builds a tea.KeyPressMsg from a single rune.
func keyPress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// specialKey builds a tea.KeyPressMsg for a named key (Enter, Backspace).
func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

// ── Init / Children ─────────────────────────────────────────────────────────

func TestInit_FetchesRoot(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})

	msg := runCmd(m.Init())
	cm, ok := msg.(ChildrenMsg)
	if !ok {
		t.Fatalf("Init() emitted %T, want ChildrenMsg", msg)
	}
	if cm.Path != "" {
		t.Errorf("ChildrenMsg.Path = %q, want \"\"", cm.Path)
	}
	if len(cm.Entries) != 2 {
		t.Errorf("len(entries) = %d, want 2 (alice, ejolly)", len(cm.Entries))
	}
}

func TestChildrenMsg_PopulatesList(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})

	m, _ = m.Update(ChildrenMsg{Path: "", Entries: p.tree[""]})
	if got := len(m.list.Items()); got != 2 {
		t.Errorf("list size after ChildrenMsg = %d, want 2", got)
	}
	if got := m.list.Title; got != "/" {
		t.Errorf("title = %q, want \"/\"", got)
	}
}

func TestChildrenMsg_ErrorShowsToastNotItems(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})

	// Seed a listing first so we can verify the previous one stays.
	m, _ = m.Update(ChildrenMsg{Path: "", Entries: p.tree[""]})

	m, _ = m.Update(ChildrenMsg{Path: "alice", Err: errStub("boom")})
	if got := len(m.list.Items()); got != 2 {
		t.Errorf("items after error = %d, want 2 (previous listing preserved)", got)
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }

// ── Navigation ──────────────────────────────────────────────────────────────

func TestDescend_OnDir_UpdatesCwdAndFetches(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})
	m, _ = m.Update(ChildrenMsg{Path: "", Entries: p.tree[""]})

	m.list.Select(0) // alice
	m, cmd := m.Update(specialKey(tea.KeyEnter))

	if m.Path() != "alice" {
		t.Errorf("cwd = %q, want \"alice\"", m.Path())
	}
	msg := runCmd(cmd)
	cm, ok := msg.(ChildrenMsg)
	if !ok {
		t.Fatalf("descend emitted %T, want ChildrenMsg", msg)
	}
	if cm.Path != "alice" {
		t.Errorf("ChildrenMsg.Path = %q, want \"alice\"", cm.Path)
	}
}

func TestDescend_OnFile_NoActionIsInert(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})
	m, _ = m.Update(ChildrenMsg{Path: "ejolly", Entries: p.tree["ejolly"]})

	m.list.Select(1) // report.pdf — no Enter-bound action registered
	m, cmd := m.Update(specialKey(tea.KeyEnter))

	if m.Path() != "" {
		t.Errorf("cwd changed to %q on file Enter; want \"\"", m.Path())
	}
	if cmd != nil {
		t.Error("file Enter returned a non-nil Cmd")
	}
}

// TestDescend_OnFile_FiresEnterAction confirms the fall-through:
// fspicker binds Action.Key = "enter" with AppliesTo = !IsDir so Enter
// on a file becomes "pick this file". Enter on a dir still descends
// (the dir branch returns early before reaching action dispatch).
func TestDescend_OnFile_FiresEnterAction(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	pick := Action{
		Key:       key.NewBinding(key.WithKeys("enter")),
		AppliesTo: func(e Entry) bool { return !e.IsDir() },
		Run: func(e Entry) tea.Cmd {
			flag.fired = true
			flag.onEntry = e
			return nil
		},
	}
	m := New(Config{Provider: p, Actions: []Action{pick}})
	m, _ = m.Update(ChildrenMsg{Path: "ejolly", Entries: p.tree["ejolly"]})

	m.list.Select(1) // report.pdf
	m, _ = m.Update(specialKey(tea.KeyEnter))

	if !flag.fired {
		t.Fatal("Enter on file did not fire Enter-bound action")
	}
	if flag.onEntry.Path() != "ejolly/report.pdf" {
		t.Errorf("action fired on %q, want ejolly/report.pdf", flag.onEntry.Path())
	}
}

// TestDescend_OnDir_DoesNotFireEnterAction confirms Enter on a dir still
// descends (no fall-through to action dispatch) — preserving normal nav.
func TestDescend_OnDir_DoesNotFireEnterAction(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	pick := Action{
		Key: key.NewBinding(key.WithKeys("enter")),
		Run: func(e Entry) tea.Cmd {
			flag.fired = true
			return nil
		},
	}
	m := New(Config{Provider: p, Actions: []Action{pick}})
	m, _ = m.Update(ChildrenMsg{Path: "", Entries: p.tree[""]})

	m.list.Select(0) // alice (dir)
	m, _ = m.Update(specialKey(tea.KeyEnter))

	if flag.fired {
		t.Error("Enter on dir fired pick action; want descend instead")
	}
	if m.Path() != "alice" {
		t.Errorf("cwd = %q, want alice", m.Path())
	}
}

func TestAscend_AtNonRoot_GoesUp(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})
	m.cwd = "ejolly/data"
	m, cmd := m.Update(specialKey(tea.KeyBackspace))

	if m.Path() != "ejolly" {
		t.Errorf("cwd = %q, want \"ejolly\"", m.Path())
	}
	cm, ok := runCmd(cmd).(ChildrenMsg)
	if !ok || cm.Path != "ejolly" {
		t.Errorf("backspace emitted %v, want ChildrenMsg{Path: ejolly}", cm)
	}
}

func TestAscend_AtRoot_IsInert(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})
	m, cmd := m.Update(specialKey(tea.KeyBackspace))

	if m.Path() != "" {
		t.Errorf("cwd = %q at root after backspace; want \"\"", m.Path())
	}
	if cmd != nil {
		t.Error("backspace at root returned non-nil Cmd")
	}
}

// ── Actions ─────────────────────────────────────────────────────────────────

// runFlag is a mutable bool we close over inside Action.Run so tests can
// assert whether Run was invoked.
type runFlag struct {
	fired   bool
	onEntry Entry
}

func deleteAction(flag *runFlag, confirm bool) Action {
	return Action{
		Key:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
		AppliesTo: func(e Entry) bool { return !e.IsDir() },
		Allowed: func(e Entry) (bool, string) {
			if strings.HasPrefix(e.Path(), "alice/") {
				return false, "cannot delete alice's files"
			}
			return true, ""
		},
		Confirm: confirm,
		Run: func(e Entry) tea.Cmd {
			flag.fired = true
			flag.onEntry = e
			return nil
		},
	}
}

func TestAction_AppliesToFalse_Ignored(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	m := New(Config{Provider: p, Actions: []Action{deleteAction(flag, false)}})
	m, _ = m.Update(ChildrenMsg{Path: "", Entries: p.tree[""]})

	m.list.Select(0) // alice (dir) — AppliesTo says no
	m, _ = m.Update(keyPress('x'))

	if flag.fired {
		t.Error("Run fired despite AppliesTo=false")
	}
}

func TestAction_AllowedFalse_ShowsReasonNoRun(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	m := New(Config{Provider: p, Actions: []Action{deleteAction(flag, false)}})
	m, _ = m.Update(ChildrenMsg{Path: "alice", Entries: p.tree["alice"]})

	m.list.Select(0) // alice/results.csv — Allowed says no
	m, cmd := m.Update(keyPress('x'))

	if flag.fired {
		t.Error("Run fired despite Allowed=false")
	}
	if cmd == nil {
		t.Fatal("expected status-toast Cmd from disallowed action")
	}
}

func TestAction_Confirm_FiresOnSecondPress(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	m := New(Config{Provider: p, Actions: []Action{deleteAction(flag, true)}})
	m, _ = m.Update(ChildrenMsg{Path: "ejolly", Entries: p.tree["ejolly"]})

	m.list.Select(1) // ejolly/report.pdf
	m, _ = m.Update(keyPress('x'))
	if flag.fired {
		t.Fatal("first press fired Run; want primed-only")
	}
	if m.pending == nil {
		t.Fatal("expected pending after first press")
	}

	m, _ = m.Update(keyPress('x'))
	if !flag.fired {
		t.Error("second press did not fire Run")
	}
	if flag.onEntry.Path() != "ejolly/report.pdf" {
		t.Errorf("Run called on %q, want ejolly/report.pdf", flag.onEntry.Path())
	}
	if m.pending != nil {
		t.Error("pending not cleared after second press")
	}
}

func TestAction_Confirm_OtherKeyCancels(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	flag := &runFlag{}
	m := New(Config{Provider: p, Actions: []Action{deleteAction(flag, true)}})
	m, _ = m.Update(ChildrenMsg{Path: "ejolly", Entries: p.tree["ejolly"]})

	m.list.Select(1)
	m, _ = m.Update(keyPress('x'))
	if m.pending == nil {
		t.Fatal("expected pending after first press")
	}

	// Move cursor — should clear pending.
	m, _ = m.Update(specialKey('j'))
	if m.pending != nil {
		t.Error("pending not cleared by cursor move")
	}

	// Second x — should re-prime, not fire.
	m.list.Select(1)
	m, _ = m.Update(keyPress('x'))
	if flag.fired {
		t.Error("Run fired after pending was cancelled")
	}
}

// ── Refresh / Status ────────────────────────────────────────────────────────

func TestRefreshMsg_RefetchesCurrentPath(t *testing.T) {
	t.Parallel()
	p := newStubProvider()
	m := New(Config{Provider: p})
	m.cwd = "ejolly"
	// Reset call log so we only count the refresh call.
	p.childrenCalls = nil

	_, cmd := m.Update(RefreshMsg{})
	cm, ok := runCmd(cmd).(ChildrenMsg)
	if !ok {
		t.Fatalf("RefreshMsg yielded %T, want ChildrenMsg", cmd)
	}
	if cm.Path != "ejolly" {
		t.Errorf("refresh fetched %q, want \"ejolly\"", cm.Path)
	}
	if len(p.childrenCalls) != 1 || p.childrenCalls[0] != "ejolly" {
		t.Errorf("Children calls = %v, want [ejolly]", p.childrenCalls)
	}
}

func TestStatusMsg_RoutesToList(t *testing.T) {
	t.Parallel()
	m := New(Config{Provider: newStubProvider()})
	// Just verify Update returns a non-nil Cmd (the list's status-bar
	// command). The exact rendering is uikit's concern.
	if _, cmd := m.Update(StatusMsg{Text: "hi", Kind: StatusSuccess}); cmd == nil {
		t.Error("StatusMsg did not produce a Cmd")
	}
}
