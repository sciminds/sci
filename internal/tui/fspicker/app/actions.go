package app

// actions.go — upload + force-upload + toggle-hidden actions for the
// filesystem picker.
//
// uploadAction binds "u" and applies to BOTH files and dirs (matching
// browse's "d" download semantics). Run records the absolute path into
// State.Picked and quits. Enter still drills into dirs via the
// browser's built-in nav; Enter on a file is inert.
//
// forceUploadAction is the uppercase-U variant: same as upload but
// also sets State.Force so the caller skips the overwrite check.
//
// toggleHiddenAction flips State.ShowHidden and refreshes the listing
// so dotfiles appear/disappear without restarting the picker.

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sciminds/cli/internal/uikit/browser"
)

// BuildActions returns upload + force-upload + toggle-hidden.
func BuildActions(state *State) []browser.Action {
	return []browser.Action{
		uploadAction(state),
		forceUploadAction(state),
		toggleHiddenAction(state),
	}
}

// uploadAction binds "u" to "select this entry and quit". Works on
// files AND dirs (browse parity: `d` downloads both).
func uploadAction(state *State) browser.Action {
	return browser.Action{
		Key: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upload")),
		Run: func(e browser.Entry) tea.Cmd {
			state.Picked = e.Path()
			state.Force = false
			return tea.Quit
		},
	}
}

// forceUploadAction binds "U" to "select + force overwrite". Caller
// reads State.Force to skip the pre-upload prefix check.
func forceUploadAction(state *State) browser.Action {
	return browser.Action{
		Key: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "force upload")),
		Run: func(e browser.Entry) tea.Cmd {
			state.Picked = e.Path()
			state.Force = true
			return tea.Quit
		},
	}
}

// toggleHiddenAction flips State.ShowHidden and refreshes.
func toggleHiddenAction(state *State) browser.Action {
	return browser.Action{
		Key: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "toggle hidden")),
		Run: func(_ browser.Entry) tea.Cmd {
			showing := state.ToggleHidden()
			text := "Hiding hidden files"
			if showing {
				text = "Showing hidden files"
			}
			return tea.Batch(
				browser.SendMsg(browser.StatusMsg{Text: text, Kind: browser.StatusInfo}),
				browser.SendMsg(browser.RefreshMsg{}),
			)
		},
	}
}
