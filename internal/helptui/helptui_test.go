package helptui

import (
	"bytes"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	testTermW = 100
	testTermH = 30
	testWait  = 2 * time.Second
	testFinal = 3 * time.Second
)

// ── Test fixtures ──────────────────────────────────────────────────────────

func testGroups() []CommandGroup {
	return []CommandGroup{
		{
			Name:     "alpha",
			Desc:     "Alpha command",
			Category: "Commands",
			FullName: "sci alpha",
			Subs: []SubCommand{
				{Name: "one", Usage: "Do thing one", FullName: "sci alpha one", CastFile: ""},
				{Name: "two", Usage: "Do thing two", FullName: "sci alpha two", CastFile: ""},
			},
		},
		{
			Name:     "beta",
			Desc:     "Beta command",
			Category: "Commands",
			FullName: "sci beta",
			Subs: []SubCommand{
				{Name: "run", Usage: "Run beta", FullName: "sci beta run", CastFile: ""},
			},
		},
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func startHelpTeatest(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := newModel(testGroups())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	tWaitFor(t, tm, "alpha")
	return tm
}

func tSendKey(tm *teatest.TestModel, key string) {
	tm.Type(key)
}

func tSendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

func tWaitFor(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithDuration(testWait))
}

func tFinalModel(t *testing.T, tm *teatest.TestModel) *model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(testFinal)).(*model)
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestHelpRenderCommands(t *testing.T) {
	tm := startHelpTeatest(t)

	fm := tFinalModel(t, tm)
	if fm.level != levelCommands {
		t.Errorf("should be at command level, got %d", fm.level)
	}
	if len(fm.commands.Items()) != 2 {
		t.Errorf("commands list has %d items, want 2", len(fm.commands.Items()))
	}
}

func TestHelpEnterGroup(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "one")

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("should be at subs level, got %d", fm.level)
	}
	if len(fm.subs.Items()) != 2 {
		t.Errorf("subs list has %d items, want 2", len(fm.subs.Items()))
	}
}

func TestHelpBackFromSubs(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "one")

	tSendSpecial(tm, tea.KeyEscape)
	tWaitFor(t, tm, "alpha")

	fm := tFinalModel(t, tm)
	if fm.level != levelCommands {
		t.Errorf("should be back at commands level, got %d", fm.level)
	}
}

func TestHelpQuitFromCommands(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendKey(tm, "q")

	fm := tFinalModel(t, tm)
	if !fm.quitting {
		t.Error("should be quitting after pressing q")
	}
}

func TestHelpQuitFromSubsWithQ(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "one")

	// q from subs goes back to commands
	tSendKey(tm, "q")
	tWaitFor(t, tm, "alpha")

	fm := tFinalModel(t, tm)
	if fm.level != levelCommands {
		t.Errorf("q from subs should go back to commands, got level %d", fm.level)
	}
}

func TestHelpNoCastStaysAtSubs(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "Do thing one")

	// Press enter on item with no cast — should stay at subs level
	tSendSpecial(tm, tea.KeyEnter)

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("should stay at subs level, got %d", fm.level)
	}
	if fm.player != nil {
		t.Error("player should be nil when no cast exists")
	}
}

func TestHelpDirectGroup(t *testing.T) {
	g := &testGroups()[0]
	m := newModelForGroup(g)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	tWaitFor(t, tm, "one")

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("direct group should start at subs level, got %d", fm.level)
	}
}

func TestHelpDirectGroupQuitOnEsc(t *testing.T) {
	g := &testGroups()[0]
	m := newModelForGroup(g)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	tWaitFor(t, tm, "one")

	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if !fm.quitting {
		t.Error("esc from direct group should quit")
	}
}
