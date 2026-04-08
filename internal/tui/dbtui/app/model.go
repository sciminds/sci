package app

// model.go — Model struct definition, constructor (NewModel), Init(), and
// small helpers that don't fit in a more specific file.

import (
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// Zone ID prefixes for clickable UI regions.
const (
	zoneTab     = "tab-"
	zoneRow     = "row-"
	zoneCol     = "col-"
	zoneHint    = "hint-"
	zoneOverlay = "overlay"
)

// Layout constants for the db table chrome.
const (
	dbChromeLines = 6 // blank + tab + underline + header + divider + status
	minTableBodyH = 2
)

// tabLoadedMsg is sent when an async tab load completes.
type tabLoadedMsg struct {
	idx int
	tab Tab
	err error
}

// Model is the single Bubble Tea model for the TUI.
// See doc.go for the full architecture overview.
type Model struct {
	// ── Backend ──────────────────────────────────────────
	store         data.DataStore
	viewLister    data.ViewLister    // non-nil when store can distinguish views from tables
	virtualLister data.VirtualLister // non-nil when store can distinguish virtual tables
	dbPath        string             // display label (file path)
	forceRO       bool               // when true, all tabs are read-only (file viewing mode)

	// ── Tab State ────────────────────────────────────────
	tabs   []Tab // one per database table; stubs until loaded
	active int   // index of the currently selected tab

	// ── Input Mode ───────────────────────────────────────
	mode      Mode
	visual    *visualState // non-nil only while in visual mode
	clipboard [][]cell     // internal clipboard for yank/paste

	// ── Overlays (nil = closed, non-nil = open) ──────────
	// At most one overlay is active at a time.
	helpVisible  bool
	notePreview  *notePreviewState
	cellEditor   *cellEditorState
	search       *rowSearchState
	tableList    *tableListState
	columnPicker *columnPickerState
	columnRename *columnRenameState

	// ── Layout & Rendering ───────────────────────────────
	width   int
	height  int
	zones   *zone.Manager
	styles  *ui.Styles
	status  statusMsg
	help    help.Model
	spinner spinner.Model
}

// NewModel creates the TUI model by introspecting the database schema.
// Only the first table is fully loaded; other tabs are stubs loaded on demand.
// When readOnly is true, all tabs are forced read-only (used for file viewing).
func NewModel(store data.DataStore, dbPath string, readOnly bool) (*Model, error) {
	tableNames, err := store.TableNames()
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	// Detect which names are SQL views or virtual tables (read-only in the TUI).
	viewLister, hasViews := store.(data.ViewLister)
	virtualLister, hasVirtuals := store.(data.VirtualLister)

	tabs := make([]Tab, len(tableNames))
	for i, name := range tableNames {
		tabs[i] = Tab{Name: name}
	}

	// Fully load only the first tab.
	if len(tabs) > 0 {
		tab, err := buildTab(store, tableNames[0])
		if err != nil {
			return nil, fmt.Errorf("build tab %q: %w", tableNames[0], err)
		}
		if readOnly || (hasViews && viewLister.IsView(tableNames[0])) ||
			(hasVirtuals && virtualLister.IsVirtual(tableNames[0])) {
			tab.ReadOnly = true
		}
		tabs[0] = tab
	}

	h := ui.NewHelp()
	h.ShowAll = true // help overlay always shows full bindings

	s := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(ui.TUI.FgAccent()))

	var vl data.ViewLister
	if hasViews {
		vl = viewLister
	}
	var vtl data.VirtualLister
	if hasVirtuals {
		vtl = virtualLister
	}

	return &Model{
		zones:         zone.New(),
		store:         store,
		viewLister:    vl,
		virtualLister: vtl,
		dbPath:        dbPath,
		styles:        ui.TUI,
		help:          h,
		spinner:       s,
		tabs:          tabs,
		active:        0,
		mode:          modeNormal,
		forceRO:       readOnly,
	}, nil
}

// ColHint overrides the default Flex/Max for a column matched by name.
type ColHint struct {
	Flex *bool // nil = keep default
	Max  int   // 0 = keep default
}

// ApplyColHints adjusts column specs on all loaded tabs.
// Keys are column names (case-sensitive, matching columnSpec.Title).
func (m *Model) ApplyColHints(hints map[string]ColHint) {
	for i := range m.tabs {
		if !m.tabs[i].Loaded {
			continue
		}
		for j := range m.tabs[i].Specs {
			h, ok := hints[m.tabs[i].Specs[j].Title]
			if !ok {
				continue
			}
			if h.Flex != nil {
				m.tabs[i].Specs[j].Flex = *h.Flex
			}
			if h.Max > 0 {
				m.tabs[i].Specs[j].Max = h.Max
			}
		}
	}
}

// SelectTab switches the active tab to the first one matching name.
// If name does not match any tab, the active tab is unchanged.
func (m *Model) SelectTab(name string) {
	for i, t := range m.tabs {
		if t.Name == name {
			m.active = i
			// Ensure the target tab is loaded.
			if !t.Loaded {
				tab, err := buildTab(m.store, name)
				if err == nil {
					if m.forceRO || (m.viewLister != nil && m.viewLister.IsView(name)) ||
						(m.virtualLister != nil && m.virtualLister.IsVirtual(name)) {
						tab.ReadOnly = true
					}
					m.tabs[i] = tab
				}
			}
			return
		}
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tea.RequestBackgroundColor)
}

// ── Internal helpers ────────────────────────────────────────────────────────

func (m *Model) effectiveTab() *Tab {
	if m.active < 0 || m.active >= len(m.tabs) {
		return nil
	}
	tab := &m.tabs[m.active]
	if !tab.Loaded || tab.Loading {
		return nil
	}
	return tab
}

func (m *Model) tabViewport(tab *Tab) tableViewport {
	if tab.CachedVP != nil {
		return *(tab.CachedVP.(*tableViewport))
	}
	normalSep := m.styles.TableSeparator().Render(" \u2502 ")
	vp := computeTableViewport(tab, m.width, normalSep)
	tab.CachedVP = &vp
	return vp
}

func (m *Model) updateTabViewport(tab *Tab) {
	visCount := m.visibleColCount(tab)
	visCursor := 0
	seen := 0
	for i, s := range tab.Specs {
		if s.HideOrder > 0 {
			continue
		}
		if i == tab.ColCursor {
			visCursor = seen
			break
		}
		seen++
	}
	ensureCursorVisible(tab, visCursor, visCount)
	tab.InvalidateVP()
}

func (m *Model) resizeTables() {
	// Reserve space for chrome lines; add 1 more if a status message is visible (2-line status bar).
	chrome := dbChromeLines
	if m.status.Text != "" {
		chrome++
	}
	if m.search != nil && !m.search.Committed {
		chrome++
	}
	tableHeight := m.height - chrome
	if tableHeight < minTableBodyH {
		tableHeight = minTableBodyH
	}
	for i := range m.tabs {
		m.tabs[i].Table.SetHeight(tableHeight)
		m.tabs[i].InvalidateVP()
	}
}

func (m *Model) hasActiveOverlay() bool {
	return m.helpVisible || m.notePreview != nil || m.cellEditor != nil || m.tableList != nil || m.columnPicker != nil || m.columnRename != nil
}

func (m *Model) dismissActiveOverlay() {
	switch {
	case m.cellEditor != nil:
		m.cellEditor = nil
	case m.helpVisible:
		m.helpVisible = false
	case m.notePreview != nil:
		m.notePreview = nil
	case m.tableList != nil:
		m.tableList = nil
	case m.columnRename != nil:
		m.columnRename = nil
	case m.columnPicker != nil:
		m.columnPicker = nil
	}
}

func (m *Model) currentTabReadOnly() bool {
	tab := m.effectiveTab()
	return tab == nil || tab.ReadOnly
}

// readOnlyReason returns a user-friendly explanation of why the current tab is read-only.
func (m *Model) readOnlyReason() string {
	tab := m.effectiveTab()
	if tab == nil {
		return "Read-only"
	}
	if m.viewLister != nil && m.viewLister.IsView(tab.Name) {
		return "Read-only (view)"
	}
	if m.virtualLister != nil && m.virtualLister.IsVirtual(tab.Name) {
		return "Read-only (virtual table)"
	}
	if m.forceRO {
		return "Read-only (file)"
	}
	return "Read-only (no primary key)"
}

// concreteStore returns the underlying *data.Store, or nil if the store
// is a different DataStore implementation (e.g. in tests).
func (m *Model) concreteStore() *data.Store {
	s, _ := m.store.(*data.Store)
	return s
}

func (m *Model) setStatusInfo(text string) {
	m.status = statusMsg{Text: text, Kind: statusInfo}
}

func (m *Model) setStatusError(text string) {
	m.status = statusMsg{Text: text, Kind: statusError}
}
