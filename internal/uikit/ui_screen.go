package uikit

import tea "charm.land/bubbletea/v2"

// Screen bundles the per-screen render and key callbacks that Bubbletea
// models typically dispatch through repeated switch statements.
//
// M is the concrete model type (e.g. *app.Model), constrained to
// tea.Model so Keys can return it directly. Using a type parameter lets
// handlers access model fields directly instead of going through
// interface assertions.
type Screen[M tea.Model] struct {
	// View renders the screen body. Receives the model and available
	// width × height (after chrome is subtracted).
	View func(m M, width, height int) string

	// Keys handles a key press on this screen. Returns an updated model
	// and optional command, same as tea.Model.Update.
	Keys func(m M, msg tea.KeyPressMsg) (tea.Model, tea.Cmd)
}

// Router maps screen IDs to Screen definitions. The zero value of a
// missing screen is safe (all callbacks are nil — View returns "" and
// Keys returns (m, nil)).
//
// S is the screen ID type (typically an int/iota enum).
// M is the model type.
//
// Router models a flat state machine: exactly one screen is active at a time
// and it owns the whole frame. It is the right tool for an app whose Update and
// View each dispatch on a single "current screen" field (see
// internal/tui/labtui/app/router.go for a worked example) — registering a
// screen wires both dispatches at once so they can't drift.
//
// It is deliberately not a compositor. An app that layers modal overlays on top
// of an always-present base (e.g. dbtui's overlay stack) has several things
// "active" simultaneously and composites them; that needs a stack/history
// abstraction Router doesn't provide. Don't force such an app through Router —
// see internal/tui/dbtui/app/doc.go ("Overlays") for that decision.
type Router[S comparable, M tea.Model] struct {
	screens map[S]Screen[M]
}

// NewRouter builds a Router from a set of screen registrations.
func NewRouter[S comparable, M tea.Model](screens map[S]Screen[M]) Router[S, M] {
	return Router[S, M]{screens: screens}
}

// View dispatches to the active screen's View callback. Returns "" if
// the screen is unregistered or View is nil.
func (r Router[S, M]) View(id S, m M, width, height int) string {
	s, ok := r.screens[id]
	if !ok || s.View == nil {
		return ""
	}
	return s.View(m, width, height)
}

// Keys dispatches to the active screen's Keys callback. Returns
// (model, nil) if the screen is unregistered or Keys is nil.
func (r Router[S, M]) Keys(id S, m M, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s, ok := r.screens[id]
	if !ok || s.Keys == nil {
		return m, nil
	}
	return s.Keys(m, msg)
}
