package app

// keys_dispatch.go — normal-mode key handler: sorting, filtering, column
// operations, mode transitions, and other keys not handled by update.go's
// shared navigation or overlay dispatch.

import (
	"strings"

	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// handleNormalModeKey processes keys specific to normal mode.
// Returns true if the key was consumed.
func (m *Model) handleNormalModeKey(k string, tab *Tab) bool {
	switch k {
	case keyD:
		m.halfPageDown(tab)

	// Sort.
	case keyS:
		tabstate.ToggleSort(tab, tab.ColCursor)
		tabstate.ApplySorts(tab)
		tab.InvalidateVP()
	case keyShiftS:
		tabstate.ClearSorts(tab)
		tabstate.ApplySorts(tab)
		tab.InvalidateVP()

	// Pin and filter.
	case keySpace:
		if c := m.selectedCell(tab); c != nil {
			val := tabstate.CellDisplayValue(*c)
			pinned := tabstate.TogglePin(tab, tab.ColCursor, val)
			tabstate.ApplyRowFilter(tab)
			tab.InvalidateVP()
			if pinned {
				m.setStatusInfo("Pinned: " + strings.TrimSpace(c.Value))
			} else {
				m.setStatusInfo("Unpinned: " + strings.TrimSpace(c.Value))
			}
		}
	case keyF:
		if tabstate.HasPins(tab) {
			tab.FilterActive = !tab.FilterActive
			tabstate.ApplyRowFilter(tab)
			tab.InvalidateVP()
			if tab.FilterActive {
				m.setStatusInfo("Filter active")
			} else {
				m.setStatusInfo("Filter preview")
			}
		} else {
			m.setStatusInfo("Pin a value with <space> to (f)ilter")
		}
	case keyBang:
		if tabstate.HasPins(tab) {
			tab.FilterInverted = !tab.FilterInverted
			tabstate.ApplyRowFilter(tab)
			tab.InvalidateVP()
			if tab.FilterInverted {
				m.setStatusInfo("Filter inverted")
			} else {
				m.setStatusInfo("Filter normal")
			}
		}
	case keyShiftSpace:
		tabstate.ClearPins(tab)
		tabstate.ApplyRowFilter(tab)
		tab.InvalidateVP()
		m.setStatusInfo("Pins cleared")

	// Expand/contract column to fit content.
	case keyE:
		if tab.ColCursor >= 0 && tab.ColCursor < len(tab.Specs) {
			tab.Specs[tab.ColCursor].Expanded = !tab.Specs[tab.ColCursor].Expanded
			tab.InvalidateVP()
		}

	// Column visibility.
	case keyC:
		if tab.ColCursor >= 0 && tab.ColCursor < len(tab.Specs) {
			maxHide := 0
			for _, s := range tab.Specs {
				if s.HideOrder > maxHide {
					maxHide = s.HideOrder
				}
			}
			hiddenName := tab.Specs[tab.ColCursor].Title
			tab.Specs[tab.ColCursor].HideOrder = maxHide + 1
			if hasSelectableCol(tab) {
				m.colRight(tab)
				if tab.Specs[tab.ColCursor].HideOrder > 0 {
					m.colLeft(tab)
				}
			}
			tab.InvalidateVP()
			m.setStatusInfo("Hidden: " + hiddenName)
		}
	case keyShiftC:
		m.openColumnPicker()

	// Column rename.
	case keyR:
		m.openColumnRename()

	// Column drop.
	case keyShiftD:
		m.dropColumn()

	// Enter = preview cell.
	case keyEnter:
		if c := m.selectedCell(tab); c != nil && c.Value != "" {
			title := ""
			if tab.ColCursor < len(tab.Specs) {
				title = tab.Specs[tab.ColCursor].Title
			}
			m.notePreview = &notePreviewState{
				Text:    c.Value,
				Title:   title,
				Overlay: ui.NewOverlay(title, c.Value, m.width, m.height),
			}
		}

	// Visual mode.
	case keyV:
		m.enterVisualMode()

	// Edit mode toggle.
	case keyI:
		if !m.currentTabReadOnly() {
			m.mode = modeEdit
		} else {
			m.setStatusInfo(m.readOnlyReason())
		}

	// Help.
	case keyQuestion:
		m.helpVisible = true

	default:
		return false
	}
	return true
}
