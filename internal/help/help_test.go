package help

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/sci/internal/tuitest"
	"github.com/urfave/cli/v3"
)

const (
	testTermW = 100
	testTermH = 30
	testWait  = 2 * time.Second
	testFinal = 3 * time.Second
)

// Thin aliases for the shared tuitest helpers.
func tSendKey(tm *teatest.TestModel, key string)    { tuitest.SendKey(tm, key) }
func tSendSpecial(tm *teatest.TestModel, code rune) { tuitest.SendSpecial(tm, code) }
func tWaitFor(t *testing.T, tm *teatest.TestModel, substr string) {
	tuitest.WaitFor(t, tm, substr, testWait)
}
func tFinalModel(t *testing.T, tm *teatest.TestModel) *model {
	return tuitest.Final[*model](t, tm, testFinal)
}

// ── Test fixtures ──────────────────────────────────────────────────────────

func testGroups() []CommandGroup {
	return []CommandGroup{
		{
			Name:     "alpha",
			Desc:     "Alpha command",
			Category: "Commands",
			FullName: "sci alpha",
			Subs: []SubCommand{
				{Name: "one", Usage: "Do thing one", FullName: "sci alpha one"},
				{Name: "two", Usage: "Do thing two", FullName: "sci alpha two"},
			},
		},
		{
			Name:     "beta",
			Desc:     "Beta command",
			Category: "Commands",
			FullName: "sci beta",
			Subs: []SubCommand{
				{Name: "run", Usage: "Run beta", FullName: "sci beta run"},
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

	// Nested: 1 direct leaf + 1 parent (not flattened) = 2 subs. Parent's Usage
	// enumerates its children so the picker surfaces the full surface.
	nested := find("nested")
	if len(nested.Subs) != 2 {
		t.Errorf("nested: got %d subs, want 2", len(nested.Subs))
	}
	var deep *SubCommand
	for i, s := range nested.Subs {
		if s.Name == "deep" {
			deep = &nested.Subs[i]
			break
		}
	}
	if deep == nil {
		names := make([]string, len(nested.Subs))
		for i, s := range nested.Subs {
			names[i] = s.Name
		}
		t.Fatalf("nested: expected sub \"deep\", got %v", names)
	}
	if !strings.Contains(deep.Usage, "gc1") || !strings.Contains(deep.Usage, "gc2") {
		t.Errorf("nested.deep: Usage should list children gc1, gc2; got %q", deep.Usage)
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

// TestHelpOpenGroupWithL proves the standardized keymap: `l` opens a group
// just like Enter (it used to page the list — now it's consistent with cloud).
func TestHelpOpenGroupWithL(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendKey(tm, "l")
	tWaitFor(t, tm, "one")

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("`l` should open the group (subs level), got %d", fm.level)
	}
}

// TestHelpBackWithH proves `h` steps back up a level, like Esc.
func TestHelpBackWithH(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "one")

	tSendKey(tm, "h")
	tWaitFor(t, tm, "alpha")

	fm := tFinalModel(t, tm)
	if fm.level != levelCommands {
		t.Errorf("`h` should go back to commands level, got %d", fm.level)
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

// TestHelpCyclesMultipleDemos covers the row that carries more than one
// recording. `sci cass canvas` is one picker row whose demos were recorded
// per leaf verb, so the overlay has to name which one is playing and offer a
// way to reach the others — without that, three of the four are as invisible
// as they were when nothing resolved to them at all.
func TestHelpCyclesMultipleDemos(t *testing.T) {
	groups := []CommandGroup{{
		Name:     "cass",
		Desc:     "Canvas LMS",
		Category: "Commands",
		FullName: "sci cass",
		Subs: []SubCommand{{
			Name:      "canvas",
			Usage:     "Canvas operations",
			FullName:  "sci cass canvas",
			CastFiles: []string{"cass-canvas-announce.cast", "cass-canvas-files.cast"},
		}},
	}}
	m := newModel(groups)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	tWaitFor(t, tm, "cass")

	tSendSpecial(tm, tea.KeyEnter) // into the subcommand list
	tWaitFor(t, tm, "operations")

	// Short substrings only: the alt screen repaints by cursor diff, so a
	// longer phrase gets fragmented across moves and never matches whole.
	tSendSpecial(tm, tea.KeyEnter) // open the first demo
	tWaitFor(t, tm, "1/2")

	tSendKey(tm, "n") // advance to the second
	tWaitFor(t, tm, "2/2")

	// The wrap is asserted on model state, not output: teatest's Output()
	// accumulates, so waiting for "1/2" again would match the first frame
	// and pass no matter what "n" did.
	tSendKey(tm, "n")
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("esc should return to subs, got level %d", fm.level)
	}
	if len(fm.casts) != 2 {
		t.Fatalf("row should carry 2 demos, got %d", len(fm.casts))
	}
	if fm.castIdx != 0 {
		t.Errorf("third `n` should wrap to demo 0, got %d", fm.castIdx)
	}
}

// TestHelpDetailPaneRendersFlagsAndExamples exercises the renderer directly.
//
// BuildGroups already extracts each subcommand's args, flags and examples off
// the live command tree. Before this view existed they were computed on every
// build and rendered nowhere, so `sci help` could tell you a command existed
// but not how to call it.
//
// Asserted against renderDetail's output rather than through teatest: the
// panel's whole point is its CONTENT, and alt-screen frames fragment long
// strings across cursor-diff repaints, so a rendered-output match can only
// ever check short tokens. Key wiring is covered separately below.
func TestHelpDetailPaneRendersFlagsAndExamples(t *testing.T) {
	g := CommandGroup{
		Name:     "zot",
		Desc:     "Zotero",
		Category: "Commands",
		FullName: "sci zot",
		Subs: []SubCommand{{
			Name:      "search",
			Usage:     "Cross-field search over the local library",
			FullName:  "sci zot search",
			ArgsUsage: "<query>",
			Flags: []Flag{
				{Names: "--library, -l", Usage: "which library to target"},
				{Names: "--limit", Usage: "max results"},
			},
			Examples: "$ sci zot search \"predictive coding\"",
		}},
	}
	m := newModel([]CommandGroup{g})
	m.Update(tea.WindowSizeMsg{Width: testTermW, Height: testTermH})
	m.openGroup(g)

	got := ansi.Strip(m.renderDetail())
	for _, want := range []string{
		"sci zot search <query>",  // invocation + declared args
		"Cross-field search",      // one-line usage
		"Flags",                   // section header
		"--library, -l",           // flag names, as declared
		"which library to target", // and their usage
		"--limit",
		"max results",
		"Examples",
		"predictive coding",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail panel missing %q\n--- panel ---\n%s", want, got)
		}
	}
}

// TestHelpDetailPaneWithoutFlagsOrExamples pins the sparse case: a command
// with neither flags nor examples still gets a usable panel — the invocation
// and usage line — rather than an empty box with a title.
func TestHelpDetailPaneWithoutFlagsOrExamples(t *testing.T) {
	g := CommandGroup{
		Name:     "alpha",
		Desc:     "Alpha",
		Category: "Commands",
		FullName: "sci alpha",
		Subs: []SubCommand{{
			Name:     "bare",
			Usage:    "A command with nothing extra",
			FullName: "sci alpha bare",
		}},
	}
	m := newModel([]CommandGroup{g})
	m.Update(tea.WindowSizeMsg{Width: testTermW, Height: testTermH})
	m.openGroup(g)

	got := ansi.Strip(m.renderDetail())
	if !strings.Contains(got, "sci alpha bare") {
		t.Errorf("sparse panel should still show the invocation:\n%s", got)
	}
	if !strings.Contains(got, "nothing extra") {
		t.Errorf("sparse panel should still show the usage line:\n%s", got)
	}
	if strings.Contains(got, "Flags") || strings.Contains(got, "Examples") {
		t.Errorf("sparse panel should omit empty sections:\n%s", got)
	}
}

// TestHelpDetailPaneKeyWiring covers the navigation half: "d" opens the panel
// from the subcommand list and "esc" closes it back to the list.
func TestHelpDetailPaneKeyWiring(t *testing.T) {
	tm := startHelpTeatest(t)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitFor(t, tm, "one")

	tSendKey(tm, "d")
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.level != levelSubs {
		t.Errorf("esc should return to subs, got level %d", fm.level)
	}
}
