package doctor

import (
	"strings"
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

func TestReccsModel_IncludesInstalled(t *testing.T) {
	t.Parallel()
	// Only rg is missing; jq and lsd are installed. All three should still
	// appear in the model so the user can see status at a glance.
	missing := map[string]bool{"rg": true}
	m := newReccsModel(testEntries, missing)
	if len(m.entries) != len(testEntries) {
		t.Errorf("entries = %d, want %d (installed entries must not be hidden)", len(m.entries), len(testEntries))
	}
	if len(m.list.Items()) != len(testEntries) {
		t.Errorf("list items = %d, want %d", len(m.list.Items()), len(testEntries))
	}
}

func TestReccsItem_TitleMarksInstalled(t *testing.T) {
	t.Parallel()
	missing := newReccsModel(testEntries, map[string]bool{"rg": true})
	items := missing.list.Items()

	// rg is missing → plain title.
	if got := items[0].(reccsItem).Title(); got != "rg" {
		t.Errorf("missing title = %q, want %q", got, "rg")
	}
	// jq is installed → title carries the OK glyph.
	jqTitle := items[1].(reccsItem).Title()
	if !strings.Contains(jqTitle, "jq") || !strings.Contains(jqTitle, "✓") {
		t.Errorf("installed title = %q, want it to contain %q and %q", jqTitle, "jq", "✓")
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
	// Even when every tool is installed, the picker still lists them all so
	// the user can confirm status — matches `sci tools list` behavior.
	m := newReccsModel(testEntries, map[string]bool{})
	if len(m.entries) != len(testEntries) {
		t.Errorf("entries = %d, want %d when nothing is missing", len(m.entries), len(testEntries))
	}
	for _, it := range m.list.Items() {
		if !it.(reccsItem).installed {
			t.Errorf("item %q must be marked installed", it.(reccsItem).entry.Name)
		}
	}
}

func TestReccsModel_EnterOnInstalledDoesNotQuit(t *testing.T) {
	t.Parallel()
	// All three tools are installed → cursor starts on the first item (rg).
	// Pressing enter must NOT pick or quit — the user should stay in the TUI
	// and see a status message about the already-installed tool.
	m := newReccsModel(testEntries, map[string]bool{})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rm := updated.(reccsModel)
	if rm.chosen != -1 {
		t.Errorf("chosen = %d, want -1 (installed tools are not installable)", rm.chosen)
	}
	if rm.quitting {
		t.Error("quitting = true, want false (enter on installed must keep the TUI open)")
	}
	if cmd != nil {
		t.Error("cmd != nil, want nil (no tea.Quit when picking an installed tool)")
	}
}

func TestReccsModel_EnterOnMissingSetsChosen(t *testing.T) {
	t.Parallel()
	// rg is missing → cursor starts on it; enter picks it for install.
	m := newReccsModel(testEntries, map[string]bool{"rg": true})
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rm := updated.(reccsModel)
	if rm.chosen != 0 {
		t.Errorf("chosen = %d, want 0 (rg is the first entry)", rm.chosen)
	}
}
