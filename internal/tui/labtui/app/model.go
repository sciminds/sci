package app

import (
	"context"
	"maps"
	"path"
	"slices"
	"strings"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/lab"
)

// screen is the top-level view state. Each screen consumes a different
// keymap; transitions are driven from Update.
type screen int

const (
	screenBrowse screen = iota
	screenConfirm
	screenTransfer
	screenError
	screenDone
)

// Model is the labtui root model. State machine across screens; selection
// is keyed by absolute remote path so it survives navigation.
type Model struct {
	cfg     *lab.Config
	backend Backend

	// browse
	cwd     string      // current remote dir (absolute path)
	entries []lab.Entry // current dir contents
	cursor  int         // index into entries
	cache   map[string][]lab.Entry
	loading bool
	loadErr error

	// selection (absolute paths, persisted across nav)
	selected map[string]bool
	// implicitSel records a path that was auto-selected by `d` when nothing
	// was explicitly selected; it gets cleared if the user backs out of the
	// confirm screen, so the implicit pick doesn't leak into later actions.
	implicitSel string

	// confirm
	sizeProbing bool
	totalBytes  int64
	sizeErr     error

	// transfer
	queue        []string // alphabetical absolute paths
	queueIdx     int
	progress     lab.Progress
	progressBar  progress.Model
	activeCh     <-chan lab.Progress
	activeDone   <-chan error
	activeCancel context.CancelFunc
	transferErr  error

	// final
	transferred int

	// resumable transfers from prior sessions; loaded once at Init and
	// after each completed queue. Drives the "press r to resume" banner.
	pending []lab.TransferEntry

	// dimensions / status
	width, height int
	screen        screen
}

// NewModel constructs a labtui model rooted at lab.ReadRoot.
func NewModel(cfg *lab.Config, backend Backend) *Model {
	pb := progress.New(progress.WithDefaultBlend())
	return &Model{
		cfg:         cfg,
		backend:     backend,
		cwd:         lab.ReadRoot,
		cache:       map[string][]lab.Entry{},
		selected:    map[string]bool{},
		screen:      screenBrowse,
		progressBar: pb,
		width:       80,
		height:      24,
	}
}

// Init implements tea.Model. Kicks off the initial directory load and a
// background scan for resumable transfers from prior sessions.
func (m *Model) Init() tea.Cmd {
	m.loading = true
	// Initialize pending to a non-nil empty slice once it loads so tests
	// (and the banner check) can distinguish "not loaded yet" from "loaded,
	// none pending".
	m.pending = nil
	return tea.Batch(loadDirCmd(m.backend, m.cwd), loadPendingCmd())
}

// ── Selection helpers ──────────────────────────────────────────────────────

// SelectedCount returns how many remote paths are currently selected.
func (m *Model) SelectedCount() int { return len(m.selected) }

// SelectedPaths returns the selection in alphabetical order.
func (m *Model) SelectedPaths() []string {
	return slices.Sorted(maps.Keys(m.selected))
}

func (m *Model) selectPath(p string)      { m.selected[p] = true }
func (m *Model) isSelected(p string) bool { return m.selected[p] }

func (m *Model) toggleSelectAtCursor() {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return
	}
	p := m.pathFor(m.entries[m.cursor])
	if m.selected[p] {
		delete(m.selected, p)
	} else {
		m.selected[p] = true
	}
}

// pathFor returns the absolute remote path for an entry under cwd.
func (m *Model) pathFor(e lab.Entry) string {
	return path.Join(m.cwd, e.Name)
}

// ── Navigation helpers ─────────────────────────────────────────────────────

// Breadcrumb renders cwd as `sciminds / data / exp1`, dropping the
// /labs/ prefix.
func (m *Model) Breadcrumb() string {
	rel := strings.TrimPrefix(m.cwd, "/labs/")
	parts := strings.Split(rel, "/")
	return strings.Join(parts, " / ")
}

// canAscend reports whether ../ is still inside ReadRoot.
func (m *Model) canAscend() bool {
	return m.cwd != lab.ReadRoot && strings.HasPrefix(m.cwd, lab.ReadRoot+"/")
}

func (m *Model) ascend() {
	if !m.canAscend() {
		return
	}
	parent := path.Dir(m.cwd)
	m.cwd = parent
	m.cursor = 0
}

func (m *Model) descendIfDir() (tea.Cmd, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil, false
	}
	e := m.entries[m.cursor]
	if !e.IsDir {
		return nil, false
	}
	m.cwd = m.pathFor(e)
	m.cursor = 0
	if cached, ok := m.cache[m.cwd]; ok {
		m.entries = cached
		return nil, true
	}
	m.entries = nil
	m.loading = true
	return loadDirCmd(m.backend, m.cwd), true
}

func (m *Model) reloadCmd() tea.Cmd {
	if cached, ok := m.cache[m.cwd]; ok {
		m.entries = cached
		return nil
	}
	m.loading = true
	return loadDirCmd(m.backend, m.cwd)
}
