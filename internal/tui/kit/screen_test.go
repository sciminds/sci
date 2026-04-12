package kit

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── Test model ─────────────────────────────────────────────────────────

type screenID int

const (
	screenA screenID = iota
	screenB
	screenMissing
)

type fakeModel struct {
	name string
}

func (m *fakeModel) Init() tea.Cmd                       { return nil }
func (m *fakeModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *fakeModel) View() tea.View                      { return tea.NewView("") }

func testRouter() Router[screenID, *fakeModel] {
	return NewRouter(map[screenID]Screen[*fakeModel]{
		screenA: {
			View: func(m *fakeModel, w, h int) string {
				return fmt.Sprintf("A:%s:%dx%d", m.name, w, h)
			},
			Keys: func(m *fakeModel, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
				m.name = "A-pressed"
				return m, nil
			},
			Title: func(m *fakeModel, w int) string {
				return fmt.Sprintf("Title-A:%d", w)
			},
			Help: "a: do thing",
		},
		screenB: {
			View: func(m *fakeModel, w, h int) string {
				return fmt.Sprintf("B:%s", m.name)
			},
			// Keys intentionally nil — tests nil-safety.
			Title: func(m *fakeModel, w int) string { return "Title-B" },
			Help:  "b: other thing",
		},
	})
}

// ── View ───────────────────────────────────────────────────────────────

func TestRouterView(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "test"}
	got := r.View(screenA, m, 80, 24)
	if got != "A:test:80x24" {
		t.Errorf("View(A) = %q, want %q", got, "A:test:80x24")
	}
}

func TestRouterViewMissing(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "test"}
	got := r.View(screenMissing, m, 80, 24)
	if got != "" {
		t.Errorf("View(missing) = %q, want empty", got)
	}
}

// ── Keys ───────────────────────────────────────────────────────────────

func TestRouterKeys(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "before"}
	result, _ := r.Keys(screenA, m, tea.KeyPressMsg{Code: 'x'})
	fm := result.(*fakeModel)
	if fm.name != "A-pressed" {
		t.Errorf("name = %q, want %q", fm.name, "A-pressed")
	}
}

func TestRouterKeysNilHandler(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "before"}
	result, cmd := r.Keys(screenB, m, tea.KeyPressMsg{Code: 'x'})
	fm := result.(*fakeModel)
	if fm.name != "before" {
		t.Errorf("name should be unchanged, got %q", fm.name)
	}
	if cmd != nil {
		t.Errorf("cmd should be nil for nil Keys handler")
	}
}

func TestRouterKeysMissing(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "before"}
	result, cmd := r.Keys(screenMissing, m, tea.KeyPressMsg{Code: 'x'})
	fm := result.(*fakeModel)
	if fm.name != "before" {
		t.Errorf("name should be unchanged, got %q", fm.name)
	}
	if cmd != nil {
		t.Errorf("cmd should be nil for missing screen")
	}
}

// ── Title ──────────────────────────────────────────────────────────────

func TestRouterTitle(t *testing.T) {
	r := testRouter()
	m := &fakeModel{name: "x"}
	if got := r.Title(screenA, m, 100); got != "Title-A:100" {
		t.Errorf("Title(A) = %q", got)
	}
	if got := r.Title(screenMissing, m, 80); got != "" {
		t.Errorf("Title(missing) = %q, want empty", got)
	}
}

// ── Help ───────────────────────────────────────────────────────────────

func TestRouterHelp(t *testing.T) {
	r := testRouter()
	if got := r.Help(screenA); got != "a: do thing" {
		t.Errorf("Help(A) = %q", got)
	}
	if got := r.Help(screenMissing); got != "" {
		t.Errorf("Help(missing) = %q, want empty", got)
	}
}
