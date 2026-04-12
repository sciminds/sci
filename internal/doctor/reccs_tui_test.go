package doctor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/brew"
)

var testEntries = []brew.BrewfileEntry{
	{Name: "rg", Type: "formula", Line: `brew "rg"`},
	{Name: "jq", Type: "formula", Line: `brew "jq"`},
	{Name: "lsd", Type: "formula", Line: `brew "lsd"`},
}

func TestReccsModel_ViewAtZeroSize(t *testing.T) {
	t.Parallel()
	missing := map[string]bool{"rg": true, "jq": true}
	m := newReccsModel(testEntries, missing)
	_ = m.View() // must not panic before WindowSizeMsg
}

func TestReccsModel_FiltersInstalled(t *testing.T) {
	t.Parallel()
	missing := map[string]bool{"rg": true}
	m := newReccsModel(testEntries, missing)
	if len(m.entries) != 1 {
		t.Errorf("entries = %d, want 1 (only rg is missing)", len(m.entries))
	}
	if m.entries[0].Name != "rg" {
		t.Errorf("entry = %q, want %q", m.entries[0].Name, "rg")
	}
}

func TestReccsModel_ChosenDefaultNegative(t *testing.T) {
	t.Parallel()
	missing := map[string]bool{"rg": true}
	m := newReccsModel(testEntries, missing)
	if m.chosen != -1 {
		t.Errorf("chosen = %d, want -1", m.chosen)
	}
}

func TestReccsModel_QuitSetsQuitting(t *testing.T) {
	t.Parallel()
	missing := map[string]bool{"rg": true}
	m := newReccsModel(testEntries, missing)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	rm := updated.(reccsModel)
	if !rm.quitting {
		t.Error("expected quitting=true after q")
	}
	if cmd == nil {
		t.Error("expected quit Cmd")
	}
}

func TestReccsItem_Title(t *testing.T) {
	t.Parallel()
	item := reccsItem{entry: brew.BrewfileEntry{Name: "rg"}, desc: "fast grep"}
	if item.Title() != "rg" {
		t.Errorf("title = %q, want %q", item.Title(), "rg")
	}
}

func TestReccsItem_FilterValue(t *testing.T) {
	t.Parallel()
	item := reccsItem{entry: brew.BrewfileEntry{Name: "rg"}, desc: "fast grep"}
	if item.FilterValue() != "rg fast grep" {
		t.Errorf("filter = %q, want %q", item.FilterValue(), "rg fast grep")
	}
}

func TestReccsModel_EmptyMissing(t *testing.T) {
	t.Parallel()
	m := newReccsModel(testEntries, map[string]bool{})
	if len(m.entries) != 0 {
		t.Errorf("entries = %d, want 0 when nothing is missing", len(m.entries))
	}
}
