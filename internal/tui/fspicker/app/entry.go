// Package app holds the fspicker implementation: a filesystem-backed
// browser.Provider, the Entry adapter over os.DirEntry, and the
// pick / toggle-hidden actions. The root fspicker package only wires
// these into uikit.Run.
package app

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/uikit"
)

// Entry wraps a filesystem entry plus its absolute path. We can't embed
// os.DirEntry because its IsDir() method clashes with how the rest of
// the package treats IsDir as an immutable boolean snapshot.
type Entry struct {
	Abs  string      // absolute path of this entry
	Name string      // base name displayed in the list
	Dir  bool        // is this a directory?
	Info fs.FileInfo // nil for broken symlinks / stat failures
}

// Title is the list-row title. Folders get a trailing slash so the
// directory hint is in the title column instead of the description.
func (e Entry) Title() string {
	if e.Dir {
		return e.Name + "/"
	}
	return e.Name
}

// Description is the dimmed metadata column.
func (e Entry) Description() string {
	if e.Dir {
		return uikit.TUI.Dim().Render("folder")
	}
	if e.Info == nil {
		return uikit.TUI.Dim().Render("?")
	}
	return uikit.TUI.Dim().Render(fmt.Sprintf("%s  %s",
		humanize.Bytes(uint64(e.Info.Size())),
		e.Info.ModTime().Format(time.DateOnly),
	))
}

// FilterValue is what the list's fuzzy filter matches on.
func (e Entry) FilterValue() string { return e.Name }

// Path returns the absolute path, satisfying browser.Entry.
func (e Entry) Path() string { return e.Abs }

// IsDir reports whether this entry is a directory.
func (e Entry) IsDir() bool { return e.Dir }
