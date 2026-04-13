package app

// collapse.go — column visibility: toggle, expand-all, gap-separator
// rendering for hidden-column indicators, and the column picker overlay
// for selectively unhiding columns.

import (
	"fmt"
	"strings"

	"github.com/samber/lo"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// gapSeparators computes per-gap separators for hidden column indicators.
func gapSeparators(
	visToFull []int,
	_ int,
	normalSep string,
) (plainSeps, collapsedSeps []string) {
	n := len(visToFull)
	if n <= 1 {
		return nil, nil
	}
	collapsedSep := ui.TUI.TableSeparator().Render(" ") +
		ui.TUI.SecondaryText().Render("\u22ef") +
		ui.TUI.TableSeparator().Render(" ")

	plainSeps = make([]string, n-1)
	collapsedSeps = make([]string, n-1)
	for i := range n - 1 {
		plainSeps[i] = normalSep
		if visToFull[i+1] > visToFull[i]+1 {
			collapsedSeps[i] = collapsedSep
		} else {
			collapsedSeps[i] = normalSep
		}
	}
	return
}

func renderHiddenBadges(
	specs []columnSpec,
	colCursor int,
) string {
	sep := ui.TUI.HeaderHint().Render(" \u00b7 ")

	var leftParts, rightParts []string
	for i, spec := range specs {
		if spec.HideOrder == 0 {
			continue
		}
		if i < colCursor {
			leftParts = append(leftParts, spec.Title)
		} else {
			rightParts = append(rightParts, spec.Title)
		}
	}
	if len(leftParts) == 0 && len(rightParts) == 0 {
		return ""
	}

	leftMarker := "  "
	if len(leftParts) > 0 {
		leftMarker = symTriLeft + " "
	}
	rightMarker := "  "
	if len(rightParts) > 0 {
		rightMarker = " " + symTriRight
	}

	var allParts []string
	for i, name := range leftParts {
		if i == 0 {
			name = leftMarker + name
		}
		if len(rightParts) == 0 && i == len(leftParts)-1 {
			name += rightMarker
		}
		allParts = append(allParts, ui.TUI.HiddenLeft().Render(name))
	}
	for i, name := range rightParts {
		if len(leftParts) == 0 && i == 0 {
			name = leftMarker + name
		}
		if i == len(rightParts)-1 {
			name += rightMarker
		}
		allParts = append(allParts, ui.TUI.HiddenRight().Render(name))
	}
	return strings.Join(allParts, sep)
}

// ── Column picker overlay ───────────────────────────────────────────────────

// hiddenColumnIndices returns spec indices where HideOrder > 0, in table order.
func hiddenColumnIndices(specs []columnSpec) []int {
	return lo.FilterMap(specs, func(s columnSpec, i int) (int, bool) {
		return i, s.HideOrder > 0
	})
}

// openColumnPicker opens the hidden-column picker.
// If exactly one column is hidden, unhides it directly (fast path).
func (m *Model) openColumnPicker() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	hidden := hiddenColumnIndices(tab.Specs)
	switch len(hidden) {
	case 0:
		m.setStatusInfo("No hidden columns")
	case 1:
		name := tab.Specs[hidden[0]].Title
		tab.Specs[hidden[0]].HideOrder = 0
		tab.InvalidateVP()
		m.setStatusInfo("Shown: " + name)
	default:
		m.columnPicker = &columnPickerState{Cursor: 0}
	}
}

const (
	colPickerMinW = 20
	colPickerMaxW = 50
)

// handleColumnPickerKey dispatches key events in the column picker overlay.
func (m *Model) handleColumnPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	tab := m.effectiveTab()
	if tab == nil {
		m.columnPicker = nil
		return nil
	}
	hidden := hiddenColumnIndices(tab.Specs)
	if len(hidden) == 0 {
		m.columnPicker = nil
		return nil
	}

	k := msg.String()
	switch k {
	case keyJ, keyDown:
		if m.columnPicker.Cursor < len(hidden)-1 {
			m.columnPicker.Cursor++
		}
	case keyK, keyUp:
		if m.columnPicker.Cursor > 0 {
			m.columnPicker.Cursor--
		}
	case keyEnter:
		idx := hidden[m.columnPicker.Cursor]
		name := tab.Specs[idx].Title
		tab.Specs[idx].HideOrder = 0
		tab.InvalidateVP()
		m.setStatusInfo("Shown: " + name)

		// Recompute and auto-close if empty.
		remaining := hiddenColumnIndices(tab.Specs)
		if len(remaining) == 0 {
			m.columnPicker = nil
		} else if m.columnPicker.Cursor >= len(remaining) {
			m.columnPicker.Cursor = len(remaining) - 1
		}
	case keyEsc, keyShiftC:
		m.columnPicker = nil
	}
	return nil
}

// buildColumnPickerOverlay renders the hidden-column picker overlay.
func (m *Model) buildColumnPickerOverlay() string {
	if m.columnPicker == nil {
		return ""
	}
	tab := m.effectiveTab()
	if tab == nil {
		return ""
	}
	hidden := hiddenColumnIndices(tab.Specs)
	if len(hidden) == 0 {
		return ""
	}

	contentW := ui.OverlayWidth(m.width, colPickerMinW, colPickerMaxW)

	var b strings.Builder
	b.WriteString(m.overlayHeader("Hidden Columns"))

	maxVisible := ui.OverlayBodyHeight(m.height, 0)
	start := 0
	if m.columnPicker.Cursor >= maxVisible {
		start = m.columnPicker.Cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(hidden) {
		end = len(hidden)
	}

	for i := start; i < end; i++ {
		spec := tab.Specs[hidden[i]]
		label := fmt.Sprintf("  %s", spec.Title)
		if i == m.columnPicker.Cursor {
			label = fmt.Sprintf("%s %s", symTriRight, spec.Title)
			b.WriteString(m.styles.AccentBold().Render(label))
		} else {
			b.WriteString(m.styles.HeaderHint().Render(label))
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}

	if len(hidden) > maxVisible {
		scrollInfo := fmt.Sprintf(" (%d/%d)", m.columnPicker.Cursor+1, len(hidden))
		b.WriteString(m.styles.TextDim().Render(scrollInfo))
	}

	b.WriteString("\n\n")
	b.WriteString(m.helpItem(symReturn, "unhide"))
	b.WriteString(m.helpSeparator())
	b.WriteString(m.helpItem(keyEsc, "close"))

	return m.styles.OverlayBox().
		Width(contentW).
		MaxHeight(lipgloss.Height(m.styles.OverlayBox().Render(strings.Repeat("\n", maxVisible+6)))).
		Render(b.String())
}
