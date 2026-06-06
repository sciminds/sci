package app

// router.go — single source of truth for per-screen View + Keys dispatch.
//
// labtui is a flat state machine: exactly one [screen] is active at a time and
// it owns the whole frame (no overlay stacking). That maps cleanly onto
// [uikit.Router], which replaces the two parallel switch statements this file
// used to require — one in View() and one in handleKey(). Registering a screen
// here wires up both at once, so the two can never drift apart.
//
// Each entry adapts an existing keyX/viewX method via a thin closure. The
// methods keep all the logic; the router only chooses which one runs.
//
// Title and Help are intentionally left nil: labtui renders its breadcrumb and
// action hints inline in each view body (and the browse hint is dynamic, so it
// can't be a static Help string). The router is used purely for body + key
// dispatch.

import (
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// router dispatches View and key handling to the active screen. Width and
// height are passed through for API symmetry; labtui's views read m.width /
// m.height directly and ignore the arguments.
var router = uikit.NewRouter(map[screen]uikit.Screen[*Model]{
	screenBrowse: {
		View: func(m *Model, _, _ int) string { return m.viewBrowse() },
		Keys: func(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.keyBrowse(msg) },
	},
	screenConfirm: {
		View: func(m *Model, _, _ int) string { return m.viewConfirm() },
		Keys: func(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.keyConfirm(msg) },
	},
	screenTransfer: {
		View: func(m *Model, _, _ int) string { return m.viewTransfer() },
		Keys: func(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.keyTransfer(msg) },
	},
	screenError: {
		View: func(m *Model, _, _ int) string { return m.viewError() },
		Keys: func(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.keyError(msg) },
	},
	screenDone: {
		View: func(m *Model, _, _ int) string { return m.viewDone() },
		Keys: func(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.keyDone(msg) },
	},
})
