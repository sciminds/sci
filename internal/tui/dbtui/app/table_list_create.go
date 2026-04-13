package app

// table_list_create.go — create and derive table sub-features of the table
// list overlay. "Create" makes an empty table; "derive" runs a SQL query and
// materialises the result as a new table or view.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
	"github.com/sciminds/cli/internal/tui/uikit"
)

// ── Create empty table ──────────────────────────────────────────────────────

// tableListStartCreate opens the create-table name dialog.
func (m *Model) tableListStartCreate() {
	tl := m.tableList
	if tl == nil {
		return
	}
	tl.Creating = true
	ed := ui.NewLineEditor("")
	tl.CreateEd = &ed
	tl.Status = ""
}

// handleTableListCreateKey handles keys while the create-table dialog is active.
func (m *Model) handleTableListCreateKey(key tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	switch key.String() {
	case keyEsc:
		tl.Creating = false
		tl.CreateEd = &ui.LineEditor{}
		return nil
	case keyEnter:
		m.tableListCommitCreate()
		return nil
	case keyBackspace:
		tl.CreateEd.Backspace()
		return nil
	case keyLeft:
		tl.CreateEd.Left()
		return nil
	case keyRight:
		tl.CreateEd.Right()
		return nil
	default:
		tl.CreateEd.InsertFromKey([]rune(key.Text), key.String())
		return nil
	}
}

// tableListCommitCreate validates and creates the empty table.
func (m *Model) tableListCommitCreate() {
	tl := m.tableList
	if tl == nil || !tl.Creating {
		return
	}

	name := tl.CreateEd.Text()
	tl.Creating = false
	tl.CreateEd = &ui.LineEditor{}

	if name == "" {
		tl.Status = "Table name cannot be empty"
		return
	}

	if !data.IsSafeIdentifier(name) {
		tl.Status = fmt.Sprintf("Invalid name: %q (alphanumerics and underscores only)", name)
		return
	}

	// Check for collision.
	for _, e := range tl.Tables {
		if e.Name == name {
			tl.Status = fmt.Sprintf("Table %q already exists", name)
			return
		}
	}

	if err := m.store.CreateEmptyTable(name); err != nil {
		tl.Status = fmt.Sprintf("Create failed: %v", err)
		return
	}

	// Refresh table entries.
	if entries, err := m.buildTableListEntries(); err == nil {
		tl.Tables = entries
	}

	// Add a stub tab.
	m.tabs = append(m.tabs, Tab{Name: name})

	tl.Status = fmt.Sprintf("Created table %q", name)
}

// buildCreateTableOverlay renders the create-table name dialog.
func (m *Model) buildCreateTableOverlay(contentW, innerW int) string {
	tl := m.tableList
	if tl == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.overlayHeader("Create Table"))

	b.WriteString("  Name: ")
	ed := tl.CreateEd
	text := ed.Text()
	if text == "" {
		b.WriteString(m.styles.Empty().Render("(type a name)"))
	} else {
		// Show text with cursor indicator.
		before := string(ed.Buf[:ed.Cursor])
		after := string(ed.Buf[ed.Cursor:])
		b.WriteString(before)
		b.WriteString(m.styles.TextBlueBold().Render("│"))
		b.WriteString(after)
	}

	b.WriteString("\n\n")
	if tl.Status != "" {
		b.WriteString(m.styles.Error().Render(tl.Status))
	} else {
		b.WriteString(" ")
	}
	b.WriteString("\n\n")

	hints := []string{
		m.helpItem(keyEnter, "create"),
		m.helpItem(keyEsc, "cancel"),
	}
	b.WriteString(strings.Join(hints, "  "))

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}

// ── Derive table / view ─────────────────────────────────────────────────────

// tableListStartDerive opens the derive-table SQL editor.
func (m *Model) tableListStartDerive() {
	tl := m.tableList
	if tl == nil {
		return
	}

	ta := textarea.New()
	ta.SetValue("SELECT ")
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(uikit.OverlayWidth(m.width, tableListMinW, tableListMaxW) - deriveSQLWidthInset)
	taH := m.height - deriveSQLChrome
	if taH < deriveSQLMinH {
		taH = deriveSQLMinH
	}
	ta.SetHeight(taH)
	ta.ShowLineNumbers = false

	ti := textinput.New()
	ti.SetValue("derived")
	ti.CharLimit = 64
	ti.Prompt = ""

	tl.Deriving = true
	tl.DeriveSQL = ta
	tl.DeriveName = ti
	tl.DeriveFocus = 0
	tl.Status = ""
}

// handleTableListDeriveKey handles keys in the derive-table overlay.
func (m *Model) handleTableListDeriveKey(msg tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	k := msg.String()
	switch k {
	case keyEsc:
		tl.Deriving = false
		return nil

	case keyTab:
		// Toggle focus between SQL and name.
		tl.DeriveFocus = 1 - tl.DeriveFocus
		if tl.DeriveFocus == 0 {
			tl.DeriveSQL.Focus()
			tl.DeriveName.Blur()
		} else {
			tl.DeriveSQL.Blur()
			tl.DeriveName.Focus()
		}
		return nil

	case keyEnter:
		// Enter = create table.
		return m.commitDerive(false)

	case keyShiftEnter:
		// Shift+Enter = create view.
		return m.commitDerive(true)
	}

	// Delegate to the focused editor.
	var cmd tea.Cmd
	if tl.DeriveFocus == 0 {
		tl.DeriveSQL, cmd = tl.DeriveSQL.Update(msg)
	} else {
		tl.DeriveName, cmd = tl.DeriveName.Update(msg)
	}
	return cmd
}

// commitDerive creates a derived table or view from the SQL query.
func (m *Model) commitDerive(asView bool) tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	query := tl.DeriveSQL.Value()
	name := tl.DeriveName.Value()

	if !data.IsSafeIdentifier(name) {
		tl.Status = fmt.Sprintf("Invalid name: %q", name)
		return nil
	}

	store := m.concreteStore()
	if store == nil {
		tl.Status = "Derive not supported for this backend"
		return nil
	}

	var err error
	var kind string
	if asView {
		err = store.CreateViewAs(name, query)
		kind = "view"
	} else {
		err = store.CreateTableAs(name, query)
		kind = "table"
	}
	if err != nil {
		tl.Status = fmt.Sprintf("Failed: %v", err)
		return nil
	}

	tl.Deriving = false

	// Add the new tab and refresh the table list.
	newTab, err := buildTab(m.store, name)
	if err != nil {
		tl.Status = fmt.Sprintf("Created %s %q but rebuild failed: %v", kind, name, err)
		return nil
	}
	m.tabs = append(m.tabs, newTab)

	// Refresh table list entries.
	summaries, err := m.store.TableSummaries()
	if err == nil {
		tl.Tables = tl.Tables[:0]
		for _, s := range summaries {
			isView := false
			if vl, ok := m.store.(data.ViewLister); ok {
				isView = vl.IsView(s.Name)
			}
			isVirtual := false
			if vtl, ok := m.store.(data.VirtualLister); ok {
				isVirtual = vtl.IsVirtual(s.Name)
			}
			tl.Tables = append(tl.Tables, tableListEntry{
				Name:      s.Name,
				Rows:      s.Rows,
				Columns:   s.Columns,
				IsView:    isView,
				IsVirtual: isVirtual,
			})
		}
	}

	tl.Status = fmt.Sprintf("Created %s %q", kind, name)
	return nil
}

// buildDeriveOverlay renders the derive-table SQL editor overlay.
func (m *Model) buildDeriveOverlay(contentW int) string {
	tl := m.tableList
	if tl == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.overlayHeader("Derive Table / View"))

	// SQL label + editor.
	sqlLabel := "SQL"
	if tl.DeriveFocus == 0 {
		sqlLabel = m.styles.TextBlue().Render("SQL")
	}
	b.WriteString(sqlLabel + "\n")
	b.WriteString(tl.DeriveSQL.View())
	b.WriteString("\n\n")

	// Name label + editor.
	nameLabel := "Name"
	if tl.DeriveFocus == 1 {
		nameLabel = m.styles.TextBlue().Render("Name")
	}
	b.WriteString(nameLabel + "\n")
	b.WriteString(tl.DeriveName.View())
	b.WriteString("\n\n")

	hints := joinWithSeparator(
		m.helpSeparator(),
		m.helpItem(keyEnter, "table"),
		m.helpItem("shift+enter", "view"),
		m.helpItem(keyTab, "switch field"),
		m.helpItem(keyEsc, "cancel"),
	)
	b.WriteString(hints)

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}
