package app

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
)

// manyTablesModel builds a model with n empty tables so list overlays have more
// rows than any reasonable terminal can show, forcing the body-budget windowing.
func manyTablesModel(t *testing.T, n int) *Model {
	t.Helper()
	stmts := make([]string, 0, n)
	for i := range n {
		stmts = append(stmts, fmt.Sprintf("CREATE TABLE t_%02d (id INTEGER PRIMARY KEY, name TEXT)", i))
	}
	m, _ := newTeatestModelWithSchema(t, stmts)
	return m
}

// TestOverlaysFitTerminalHeight is the regression guard at the dbtui layer:
// every overlay that routes its body through uikit.OverlayBodyBudget must
// render no taller than the terminal. This catches a prefix/suffix that drifts
// out of sync with the real prefix+body+suffix assembly — a mismatch the
// uikit-level helper tests can't see, because they don't know how dbtui
// concatenates the pieces.
func TestOverlaysFitTerminalHeight(t *testing.T) {
	t.Parallel()

	// Heights above the OverlayMinH / minimum-editor-height floor, where the
	// overlay is expected to fit. (Below the floor it may exceed termH by design;
	// that is the floor guaranteeing a usable body on a tiny terminal.)
	for _, termH := range []int{20, 30, 50} {
		t.Run(fmt.Sprintf("h%d", termH), func(t *testing.T) {
			t.Parallel()
			m := manyTablesModel(t, 40)
			m.width = 100
			m.height = termH

			cases := []struct {
				name  string
				build func() string
			}{
				{"tableList", func() string {
					m.openTableList()
					return m.buildTableListOverlay()
				}},
				{"derive", func() string {
					m.openTableList()
					m.tableListStartDerive()
					return m.buildTableListOverlay()
				}},
			}

			for _, tc := range cases {
				overlay := tc.build()
				if overlay == "" {
					t.Fatalf("%s overlay rendered empty", tc.name)
				}
				if h := lipgloss.Height(overlay); h > termH {
					t.Errorf("%s overlay height %d exceeds terminal height %d", tc.name, h, termH)
				}
			}
		})
	}
}
