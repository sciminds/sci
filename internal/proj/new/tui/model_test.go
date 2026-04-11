package tui

import (
	"testing"

	projnew "github.com/sciminds/cli/internal/proj/new"
)

// TestViewAtZeroSize ensures the proj config TUI can render View() before any
// WindowSizeMsg arrives (width=0, height=0).
func TestViewAtZeroSize(t *testing.T) {
	t.Parallel()
	files := []projnew.ConfigFile{
		{Path: ".gitignore", Changed: true},
		{Path: "pyproject.toml", Changed: false, Exists: true},
	}
	m := New(Options{Dir: "/tmp/test", Files: files})
	_ = m.View() // must not panic
}
