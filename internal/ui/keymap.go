package ui

// keymap.go — reusable Bubble Tea key bindings (quit, arrows, tab) that each
// TUI model composes into its own KeyMap.

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// ── Shared key bindings ─────────────────────────────────────────────────────
// Reusable across all TUIs. Each TUI composes these into its own KeyMap.

var (
	BindQuit = key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q/esc", "quit"),
	)
	BindUp = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	)
	BindDown = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	)
	BindEnter = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	)
	BindHelp = key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	)
)

// NewHelp creates a help.Model styled from the shared palette.
func NewHelp() help.Model {
	h := help.New()
	h.Styles = help.Styles{
		ShortKey:       TUI.Keycap(),
		ShortDesc:      TUI.TextMid(),
		ShortSeparator: TUI.TextDim(),
		FullKey:        TUI.Keycap(),
		FullDesc:       TUI.TextMid(),
		FullSeparator:  TUI.TextDim(),
	}
	return h
}
