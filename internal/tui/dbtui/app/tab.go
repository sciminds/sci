package app

// tab.go — Tab lifecycle: building tabs from database tables, lazy loading,
// switching between tabs, and restoring state after mutations.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/uikit"
)

// buildTab creates a Tab from a database table by introspecting its schema and data.
func buildTab(store data.DataStore, tableName string) (Tab, error) {
	pragmaCols, err := store.TableColumns(tableName)
	if err != nil {
		return Tab{}, fmt.Errorf("columns for %q: %w", tableName, err)
	}

	specs := make([]columnSpec, 0, len(pragmaCols))
	for _, pc := range pragmaCols {
		kind := sqlTypeToKind(pc.Type)
		if pc.PK > 0 {
			kind = cellReadonly
		}
		spec := columnSpec{
			Title:  pc.Name,
			DBName: pc.Name,
			Min:    8,
			Flex:   true,
			Align:  alignLeft,
			Kind:   kind,
		}
		// Right-align numeric columns.
		if spec.Kind == cellInteger || spec.Kind == cellReal {
			spec.Align = alignRight
		}
		specs = append(specs, spec)
	}

	columns := specsToColumns(specs)
	tbl := newTable(columns)

	_, rowData, nullFlags, rowIDs, err := store.QueryTable(tableName)
	if err != nil {
		return Tab{}, fmt.Errorf("query %q: %w", tableName, err)
	}

	cellRows := make([][]cell, len(rowData))
	tableRows := make([]table.Row, len(rowData))
	meta := make([]rowMeta, len(rowData))
	for i, row := range rowData {
		cells := make([]cell, len(row))
		tRow := make(table.Row, len(row))
		for j, val := range row {
			isNull := j < len(nullFlags[i]) && nullFlags[i][j]
			kind := cellText
			if j < len(specs) {
				kind = specs[j].Kind
			}
			cells[j] = cell{Value: val, Kind: kind, Null: isNull}
			tRow[j] = val
		}
		cellRows[i] = cells
		tableRows[i] = tRow
		rid := int64(0)
		if i < len(rowIDs) {
			rid = rowIDs[i]
		}
		meta[i] = rowMeta{ID: uint(i), RowID: rid}
	}

	tbl.SetRows(tableRows)

	// Start cursor on first non-readonly column.
	initialCol := 0
	for i, s := range specs {
		if s.Kind != cellReadonly {
			initialCol = i
			break
		}
	}

	tab := Tab{
		Name:         tableName,
		Table:        tbl,
		Rows:         meta,
		Specs:        specs,
		CellRows:     cellRows,
		ColCursor:    initialCol,
		ReadOnly:     false,
		Loaded:       true,
		FullRows:     tableRows,
		FullMeta:     meta,
		FullCellRows: cellRows,
	}

	tabstate.ApplySorts(&tab)

	return tab, nil
}

// sqlTypeToKind maps SQL type names to cellKind.
func sqlTypeToKind(sqlType string) cellKind {
	upper := strings.ToUpper(sqlType)
	switch {
	case strings.Contains(upper, "INT"):
		return cellInteger
	case strings.Contains(upper, "REAL"),
		strings.Contains(upper, "FLOA"),
		strings.Contains(upper, "DOUB"):
		return cellReal
	case strings.Contains(upper, "BOOL"):
		return cellText // display as text, not numeric
	case strings.Contains(upper, "BLOB"):
		return cellReadonly
	default:
		return cellText
	}
}

func specsToColumns(specs []columnSpec) []table.Column {
	return lo.Map(specs, func(spec columnSpec, _ int) table.Column {
		return table.Column{
			Title: spec.Title,
			Width: spec.Min,
		}
	})
}

func newTable(columns []table.Column) table.Model {
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)
	s := table.DefaultStyles()
	s.Header = uikit.TUI.TableHeader()
	s.Selected = uikit.TUI.Base()
	s.Cell = uikit.TUI.Base()
	t.SetStyles(s)
	km := t.KeyMap
	km.PageDown.SetKeys(keyPgDown)
	km.PageUp.SetKeys(keyPgUp)
	t.KeyMap = km
	return t
}

// switchToTab changes the active tab, exiting visual/edit mode if needed,
// and triggers a lazy load if the target tab hasn't been loaded yet.
func (m *Model) switchToTab(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.tabs) {
		return nil
	}
	m.active = idx
	// Exit visual/edit mode when switching tabs.
	if m.mode == modeVisual {
		m.exitVisualMode()
	}
	if m.mode == modeEdit && m.tabs[idx].ReadOnly {
		m.mode = modeNormal
	}
	return m.triggerTabLoad(idx)
}

// triggerTabLoad starts an async load for a stub tab. Returns nil if already loaded/loading.
func (m *Model) triggerTabLoad(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.tabs) {
		return nil
	}
	tab := &m.tabs[idx]
	if tab.Loaded || tab.Loading {
		return nil
	}
	tab.Loading = true
	store := m.store
	name := tab.Name
	return func() tea.Msg {
		t, err := buildTab(store, name)
		return tabLoadedMsg{idx: idx, tab: t, err: err}
	}
}

// handleTabLoaded processes the result of an async tab load.
func (m *Model) handleTabLoaded(msg tabLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.idx < 0 || msg.idx >= len(m.tabs) {
		return m, nil
	}
	m.tabs[msg.idx].Loading = false
	if msg.err != nil {
		m.setStatusError(fmt.Sprintf("Load %q: %v", m.tabs[msg.idx].Name, msg.err))
		return m, nil
	}
	if m.forceRO || (m.viewLister != nil && m.viewLister.IsView(msg.tab.Name)) ||
		(m.virtualLister != nil && m.virtualLister.IsVirtual(msg.tab.Name)) {
		msg.tab.ReadOnly = true
	}
	msg.tab.Table.SetHeight(m.tabs[m.active].Table.Height())
	m.tabs[msg.idx] = msg.tab
	m.resizeTables()
	return m, nil
}

func (m *Model) nextTab() tea.Cmd {
	if len(m.tabs) > 0 {
		return m.switchToTab((m.active + 1) % len(m.tabs))
	}
	return nil
}

func (m *Model) prevTab() tea.Cmd {
	if len(m.tabs) > 0 {
		return m.switchToTab((m.active - 1 + len(m.tabs)) % len(m.tabs))
	}
	return nil
}

// rebuildTab re-fetches a tab from the database and restores cursor/sort/filter state.
// It replaces m.tabs[idx] in place. Returns an error string suitable for status display.
func (m *Model) rebuildTab(idx int) error {
	old := &m.tabs[idx]
	oldCursor := old.Table.Cursor()
	newTab, err := buildTab(m.store, old.Name)
	if err != nil {
		return fmt.Errorf("rebuild failed: %w", err)
	}
	restoreTabState(&newTab, old, oldCursor)
	m.tabs[idx] = newTab
	return nil
}
