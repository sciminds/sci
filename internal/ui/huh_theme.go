package ui

// huh_theme.go — custom [huh.Theme] built from the Wong palette. Isolated
// from styles.go so huh is not a transitive dependency for all ui importers.

import (
	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// HuhTheme returns a huh.ThemeFunc built from the project's Wong colorblind-safe palette.
// Separate from styles.go so huh is not a transitive dependency for all ui importers.
func HuhTheme() huh.ThemeFunc {
	return func(isDark bool) *huh.Styles {
		p := TUI.Palette()
		t := huh.ThemeBase(isDark)

		f := &t.Focused
		f.Base = f.Base.BorderForeground(p.Border)
		f.Card = f.Base
		f.Title = f.Title.Foreground(p.Accent).Bold(true)
		f.NoteTitle = f.NoteTitle.Foreground(p.Accent).Bold(true)
		f.Description = f.Description.Foreground(p.TextMid)
		f.ErrorIndicator = f.ErrorIndicator.Foreground(p.Danger)
		f.ErrorMessage = f.ErrorMessage.Foreground(p.Danger)

		// Select
		f.SelectSelector = f.SelectSelector.Foreground(p.Secondary)
		f.NextIndicator = f.NextIndicator.Foreground(p.Secondary)
		f.PrevIndicator = f.PrevIndicator.Foreground(p.Secondary)
		f.Option = f.Option.Foreground(p.TextBright)
		f.SelectedOption = f.SelectedOption.Foreground(p.Success)
		f.SelectedPrefix = lipgloss.NewStyle().Foreground(p.Success).SetString("✓ ")
		f.UnselectedPrefix = lipgloss.NewStyle().Foreground(p.TextMid).SetString("• ")
		f.UnselectedOption = f.UnselectedOption.Foreground(p.TextBright)

		// Multi-select
		f.MultiSelectSelector = f.MultiSelectSelector.Foreground(p.Secondary)

		// Buttons
		f.FocusedButton = f.FocusedButton.Foreground(p.OnAccent).Background(p.Accent)
		f.Next = f.FocusedButton
		f.BlurredButton = f.BlurredButton.Foreground(p.TextMid).Background(p.Surface)

		// Text input
		f.TextInput.Cursor = f.TextInput.Cursor.Foreground(p.Accent)
		f.TextInput.Placeholder = f.TextInput.Placeholder.Foreground(p.TextDim)
		f.TextInput.Prompt = f.TextInput.Prompt.Foreground(p.Secondary)
		f.TextInput.Text = f.TextInput.Text.Foreground(p.TextBright)

		// Blurred: same styles but hidden border
		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()

		// Group
		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description

		return t
	}
}

// HuhKeyMap returns a huh.KeyMap with esc and q added to the Quit binding
// so users can cancel forms with ctrl+c, esc, or q.
func HuhKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc", "q"))
	return km
}
