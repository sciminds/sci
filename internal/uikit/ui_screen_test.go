package uikit

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
		},
		screenB: {
			View: func(m *fakeModel, w, h int) string {
				return fmt.Sprintf("B:%s", m.name)
			},
			// Keys intentionally nil — tests nil-safety.
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
