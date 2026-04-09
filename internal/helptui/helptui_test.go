package helptui

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/urfave/cli/v3"
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

// testRoot returns a cli.Command tree that mirrors the real app structure:
// leaf commands, parent+children, and nested grandchildren. The command must
// be Run so urfave/cli injects its automatic "help" subcommands — the exact
// condition that caused the empty-list bug.
func testRoot() *cli.Command {
	noop := func(context.Context, *cli.Command) error { return nil }
	return &cli.Command{
		Name:            "sci",
		HideHelpCommand: true,
		Action:          noop,
		Commands: []*cli.Command{
			{
				Name: "leaf",
				// Leaf command with no subcommands — gets a synthetic sub.
				Usage:    "A leaf command",
				Category: "Commands",
				Action:   noop,
			},
			{
				Name:     "parent",
				Usage:    "A parent command",
				Category: "Commands",
				Commands: []*cli.Command{
					{Name: "child1", Usage: "First child", Action: noop},
					{Name: "child2", Usage: "Second child", Action: noop},
				},
			},
			{
				Name:     "nested",
				Usage:    "Has both direct and nested children",
				Category: "Commands",
				Commands: []*cli.Command{
					{Name: "direct", Usage: "Direct sub", Action: noop},
					{
						Name:  "deep",
						Usage: "Has grandchildren",
						Commands: []*cli.Command{
							{Name: "gc1", Usage: "Grandchild 1", Action: noop},
							{Name: "gc2", Usage: "Grandchild 2", Action: noop},
						},
					},
				},
			},
		},
	}
}

// initRoot runs the root command with --help so urfave/cli initialises the
// tree (injecting auto-help commands, setting parents, etc.) without
// executing any real action.
func initRoot(t *testing.T) *cli.Command {
	t.Helper()
	root := testRoot()
	// Run with --help triggers init but prints help to stdout; silence it.
	root.Writer = io.Discard
	_ = root.Run(context.Background(), []string{"sci", "--help"})
	return root
}

func TestBuildGroupsAfterInit(t *testing.T) {
	root := initRoot(t)
	groups := BuildGroups(root)

	find := func(name string) *CommandGroup {
		for i := range groups {
			if groups[i].Name == name {
				return &groups[i]
			}
		}
		t.Fatalf("group %q not found", name)
		return nil
	}

	// Leaf command should have exactly 1 synthetic sub.
	leaf := find("leaf")
	if len(leaf.Subs) != 1 {
		t.Errorf("leaf: got %d subs, want 1", len(leaf.Subs))
	}

	// Parent with 2 children should have exactly 2 subs (not 0).
	parent := find("parent")
	if len(parent.Subs) != 2 {
		t.Errorf("parent: got %d subs, want 2", len(parent.Subs))
	}

	// Nested: 1 direct child + 2 flattened grandchildren = 3 subs.
	nested := find("nested")
	if len(nested.Subs) != 3 {
		t.Errorf("nested: got %d subs, want 3", len(nested.Subs))
	}
	// Verify flattened names use "deep gc1" format.
	found := false
	for _, s := range nested.Subs {
		if s.Name == "deep gc1" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(nested.Subs))
		for i, s := range nested.Subs {
			names[i] = s.Name
		}
		t.Errorf("nested: expected flattened sub \"deep gc1\", got %v", names)
	}
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
