package app

// actions.go — pick + toggle-hidden actions for the filesystem picker.
//
// pickAction binds Enter and applies only to files: dirs go through the
// browser's built-in nav (descend). Run records the absolute path into
// State.Picked and quits the program; the root fspicker package reads
// the path from the returned final model.
//
// toggleHiddenAction flips State.ShowHidden and refreshes the listing
// so dotfiles appear/disappear without restarting the picker.

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sciminds/cli/internal/uikit/browser"
)

// BuildActions returns the pick + toggle-hidden pair.
func BuildActions(state *State) []browser.Action {
	return []browser.Action{
		pickAction(state),
		toggleHiddenAction(state),
	}
}

// pickAction binds Enter on a file to "select this file and quit".
// The Help line shows "⏎ pick" so users know Enter has dual roles
// (descend on dirs, pick on files — both are "open").
func pickAction(state *State) browser.Action {
	return browser.Action{
		Key:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "pick")),
		AppliesTo: notDir,
		Run: func(e browser.Entry) tea.Cmd {
			state.Picked = e.Path()
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

// notDir is the file-only AppliesTo predicate.
func notDir(e browser.Entry) bool { return !e.IsDir() }
