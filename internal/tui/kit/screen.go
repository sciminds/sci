package kit

import tea "charm.land/bubbletea/v2"

// Screen bundles the four per-screen callbacks that Bubbletea models
// typically dispatch through repeated switch statements.
//
// M is the concrete model type (e.g. *app.Model). Using a type parameter
// lets handlers access model fields directly instead of going through
// interface assertions.
type Screen[M any] struct {
	// View renders the screen body. Receives the model and available
	// width × height (after chrome is subtracted).
	View func(m M, width, height int) string

	// Keys handles a key press on this screen. Returns an updated model
	// and optional command, same as tea.Model.Update.
	Keys func(m M, msg tea.KeyPressMsg) (tea.Model, tea.Cmd)

	// Title renders the title bar text. Receives the model and width.
	Title func(m M, width int) string

	// Help is a static help-hint string shown in the status bar when
	// there is no transient status message.
	Help string
}

// Router maps screen IDs to Screen definitions. The zero value of a
// missing screen is safe (all callbacks are nil — View returns "",
// Keys returns (m, nil), etc.).
//
// S is the screen ID type (typically an int/iota enum).
// M is the model type.
type Router[S comparable, M any] struct {
	screens map[S]Screen[M]
}

// NewRouter builds a Router from a set of screen registrations.
func NewRouter[S comparable, M any](screens map[S]Screen[M]) Router[S, M] {
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
		return any(m).(tea.Model), nil
	}
	return s.Keys(m, msg)
}

// Title dispatches to the active screen's Title callback. Returns ""
// if unregistered or nil.
func (r Router[S, M]) Title(id S, m M, width int) string {
	s, ok := r.screens[id]
	if !ok || s.Title == nil {
		return ""
	}
	return s.Title(m, width)
}

// Help returns the active screen's help string. Returns "" if
// unregistered.
func (r Router[S, M]) Help(id S) string {
	s, ok := r.screens[id]
	if !ok {
		return ""
	}
	return s.Help
}
