package uikit

// ui_actionmenu.go — single-select action picker rendered inside an OverlayBox.
// Used for context menus in TUI browsers (e.g. "Copy BibTeX / Open PDF / Open
// in Zotero"). The parent model owns compositing via [Compose].

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Action is a single entry in an [ActionMenu].
type Action struct {
	Name     string // "Copy BibTeX"
	Hint     string // "to clipboard" — dimmed suffix, optional
	Disabled string // non-empty = grayed out, shows this as reason
}

// ActionMenu is a single-select cursor menu for "pick one action" overlays.
// It renders inside an [OverlayBox] and the parent composites it over the
// background via [Compose].
//
// Usage:
//
//	menu := uikit.NewActionMenu("Smith 2024", []uikit.Action{
//	    {Name: "Copy BibTeX", Hint: "to clipboard"},
//	    {Name: "Open PDF",    Hint: "in default viewer"},
//	    {Name: "Open in Zotero"},
//	})
//
//	// Update: menu, cmd = menu.Update(msg)
//	// Query:  menu.Picked()    → index or -1
//	//         menu.Dismissed() → true on esc
//	// View:   uikit.Compose(menu.View(termW, termH), bg)
type ActionMenu struct {
	title     string
	actions   []Action
	cursor    int
	picked    int
	dismissed bool
}

// NewActionMenu creates an action menu. The cursor starts on the first
// enabled action.
func NewActionMenu(title string, actions []Action) ActionMenu {
	m := ActionMenu{
		title:   title,
		actions: actions,
		picked:  -1,
	}
	// Start cursor on first enabled item.
	m.cursor = m.nextEnabled(0, 1)
	return m
}

// ── Queries ──────────────────────────────────────────────────────────

// Picked returns the index of the chosen action, or -1 if none picked yet.
func (m ActionMenu) Picked() int { return m.picked }

// Dismissed returns true if the user pressed esc or ctrl+c.
func (m ActionMenu) Dismissed() bool { return m.dismissed }

// Cursor returns the current cursor index.
func (m ActionMenu) Cursor() int { return m.cursor }

// ── Update ───────────────────────────────────────────────────────────

// Update handles key messages. Returns the updated menu and nil cmd
// (pure state transitions, no side effects).
func (m ActionMenu) Update(msg tea.Msg) (ActionMenu, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case KeyDown, KeyJ:
		if next := m.nextEnabled(m.cursor+1, 1); next >= 0 {
			m.cursor = next
		}
	case KeyUp, KeyK:
		if prev := m.nextEnabled(m.cursor-1, -1); prev >= 0 {
			m.cursor = prev
		}
	case KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.actions) && m.actions[m.cursor].Disabled == "" {
			m.picked = m.cursor
		}
	case KeyEsc:
		m.dismissed = true
	case KeyCtrlC:
		m.dismissed = true
	}

	return m, nil
}

// nextEnabled scans from start in direction dir (+1 or -1) and returns the
// index of the first enabled action, or -1 if none found.
func (m ActionMenu) nextEnabled(start, dir int) int {
	for i := start; i >= 0 && i < len(m.actions); i += dir {
		if m.actions[i].Disabled == "" {
			return i
		}
	}
	return -1
}

// ── View ─────────────────────────────────────────────────────────────

// View renders the menu as a styled [OverlayBox]. The parent composites
// the result over the background via [Compose]. Long action lists are
// truncated with a trailing ellipsis when they don't fit termH.
func (m ActionMenu) View(termW, termH int) string {
	var lines []string
	for i, a := range m.actions {
		lines = append(lines, m.renderAction(i, a))
	}
	return OverlayBox{
		Title: m.title,
		Body:  strings.Join(lines, "\n"),
		Hints: []string{"enter select", "esc close"},
	}.Render(termW, termH)
}

func (m ActionMenu) renderAction(i int, a Action) string {
	isCursor := i == m.cursor

	if a.Disabled != "" {
		line := fmt.Sprintf("    %s  %s", a.Name, a.Disabled)
		return TUI.Dim().Render(line)
	}

	cursor := "  "
	if isCursor {
		cursor = TUI.TextBlue().Render(" " + IconCursor)
	}

	name := a.Name
	if isCursor {
		name = TUI.Cursor().Render(" " + name + " ")
	}

	hint := ""
	if a.Hint != "" {
		hint = "  " + TUI.Dim().Render(a.Hint)
	}

	return fmt.Sprintf("%s  %s%s", cursor, name, hint)
}
