package uikit

import (
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
	t.Parallel()
	t.Run("selectList", func(t *testing.T) {
		items := []SelectItem{selectTestItem("a"), selectTestItem("b")}
		m := NewSelectList(items)
		_ = m.View() // must not panic
	})

	t.Run("runnerModel/spinner", func(t *testing.T) {
		m := newRunnerModel("test", false)
		_ = m.View() // must not panic at zero state
	})

	t.Run("runnerModel/progress", func(t *testing.T) {
		m := newRunnerModel("test", true)
		_ = m.View() // must not panic at zero state
	})
}
