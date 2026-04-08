package ui

// keymap.go — help.Model factory styled from the shared palette.

import (
	"charm.land/bubbles/v2/help"
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
