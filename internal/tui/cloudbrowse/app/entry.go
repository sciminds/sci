// Package app holds the cloudbrowse implementation: the browser.Provider
// over a mutable []cloud.ObjectInfo, the per-entry delete/copy/download
// actions, and the Entry adapter that wraps share.TreeEntry for display.
// The root cloudbrowse package only wires these into uikit.Run.
package app

// entry.go — adapts share.TreeEntry to the browser.Entry interface.
// Folders render with a trailing slash so the directory hint is in the
// title column instead of the description column.

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/uikit"
)

// Entry wraps a share.TreeEntry. The exported field stays accessible to
// callers that need raw metadata (size, owner, URL); the methods make
// it satisfy both list.Item and browser.Entry.
//
// We can't embed TreeEntry because it has an IsDir field that would
// collide with the IsDir() method required by browser.Entry.
type Entry struct {
	T share.TreeEntry
}

// Title is the list-row title. Folders get a trailing slash so the
// directory hint is in the title column instead of the description.
func (e Entry) Title() string {
	if e.T.IsDir {
		return e.T.Name + "/"
	}
	return e.T.Name
}

// Description is the list-row dimmed metadata. Folders just say
// "folder"; files surface their detected type + humanised size.
func (e Entry) Description() string {
	if e.T.IsDir {
		return uikit.TUI.Dim().Render("folder")
	}
	return uikit.TUI.Dim().Render(fmt.Sprintf("%s  %s",
		share.DetectFileType(e.T.Name),
		humanize.Bytes(uint64(e.T.Size)),
	))
}

// FilterValue is what the list's fuzzy filter matches on.
func (e Entry) FilterValue() string { return e.T.Name }

// Path returns the full bucket key, satisfying browser.Entry.
func (e Entry) Path() string { return e.T.Key }

// IsDir reports whether this entry is a folder, satisfying browser.Entry.
func (e Entry) IsDir() bool { return e.T.IsDir }
