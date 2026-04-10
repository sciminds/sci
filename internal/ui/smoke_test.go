package ui

import (
	"bytes"
	"testing"
)

type selectTestItem string

func (s selectTestItem) SelectTitle() string { return string(s) }

// TestViewAtZeroSize ensures every TUI model can render View() before any
// WindowSizeMsg arrives (width=0, height=0). Bubble Tea calls View()
// immediately on startup — if View() panics at zero dimensions, users
// see a crash on launch.
//
// Add a subtest here for every new Bubble Tea model in this package.
func TestViewAtZeroSize(t *testing.T) {
	t.Run("selectList", func(t *testing.T) {
		items := []SelectItem{selectTestItem("a"), selectTestItem("b")}
		m := NewSelectList(items)
		_ = m.View() // must not panic
	})

	t.Run("overlay", func(t *testing.T) {
		o := NewOverlay("title", "content", 0, 0)
		_ = o.View() // must not panic
	})

	t.Run("tickRenderer", func(t *testing.T) {
		var buf bytes.Buffer
		r := newTickRenderer(&buf, "test")
		r.render() // must not panic at zero state
	})
}
