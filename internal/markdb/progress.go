package markdb

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/ui"
)

const (
	progressPadding  = 2
	progressMaxWidth = 60
)

// ingestDoneMsg is sent when the background ingest completes.
type ingestDoneMsg struct {
	stats    *IngestStats
	resolved int
	broken   int
	err      error
}

// progressMsg is sent from the ingest callback to update the progress bar.
type progressMsg struct {
	phase   string
	current int
	total   int
}

// ingestModel drives a progress bar during ingestion.
type ingestModel struct {
	store    *Store
	root     string
	progress progress.Model
	phase    string
	current  int
	total    int
	done     bool
	result   *ingestDoneMsg
	sub      chan progressMsg
}

func newIngestModel(store *Store, root string) ingestModel {
	pal := ui.TUI.Palette()
	p := progress.New(progress.WithColors(pal.Accent, pal.Secondary), progress.WithScaled(true))
	return ingestModel{
		store:    store,
		root:     root,
		progress: p,
		phase:    "starting",
		sub:      make(chan progressMsg, 256),
	}
}

func (m ingestModel) Init() tea.Cmd {
	return tea.Batch(m.runIngest(), m.waitForProgress())
}

func (m ingestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Don't allow quitting mid-ingest.
		return m, nil

	case tea.WindowSizeMsg:
		w := msg.Width - progressPadding*2 - 4
		if w > progressMaxWidth {
			w = progressMaxWidth
		}
		m.progress.SetWidth(w)
		return m, nil

	case progressMsg:
		m.phase = msg.phase
		m.current = msg.current
		m.total = msg.total
		return m, m.waitForProgress()

	case ingestDoneMsg:
		m.done = true
		m.result = &msg
		return m, tea.Quit

	default:
		return m, nil
	}
}

func (m ingestModel) View() tea.View {
	pad := strings.Repeat(" ", progressPadding)

	if m.done {
		return tea.NewView("") // final output handled by cmdutil.Output
	}

	var percent float64
	var label string

	switch m.phase {
	case "scan":
		// During scan we don't know the total, so show an indeterminate-style bar.
		// Clamp to 50% during scan phase to leave room for upsert.
		percent = 0.0
		label = fmt.Sprintf("scanning… %d files found", m.current)
	case "upsert":
		if m.total > 0 {
			percent = float64(m.current) / float64(m.total)
		}
		label = fmt.Sprintf("ingesting %d/%d", m.current, m.total)
	default:
		label = m.phase
	}

	return tea.NewView("\n" + pad + m.progress.ViewAs(percent) + "\n" +
		pad + ui.TUI.Dim().Render(label) + "\n")
}

// runIngest starts the ingest in a goroutine and sends the result back.
func (m ingestModel) runIngest() tea.Cmd {
	return func() tea.Msg {
		onProgress := func(phase string, current, total int) {
			m.sub <- progressMsg{phase: phase, current: current, total: total}
		}

		stats, err := m.store.IngestWithProgress(m.root, onProgress)
		if err != nil {
			return ingestDoneMsg{err: err}
		}

		resolved, broken, linkErr := m.store.ResolveLinks()
		if linkErr != nil {
			return ingestDoneMsg{stats: stats, err: linkErr}
		}

		return ingestDoneMsg{stats: stats, resolved: resolved, broken: broken}
	}
}

// waitForProgress listens for the next progress update from the channel.
func (m ingestModel) waitForProgress() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.sub
		if !ok {
			return nil
		}
		return msg
	}
}

// RunIngestWithProgress runs the ingest with a bubbletea progress bar.
// Returns the result for cmdutil.Output.
func RunIngestWithProgress(store *Store, root, dbPath string) (IngestCmdResult, error) {
	m := newIngestModel(store, root)
	finalModel, err := tea.NewProgram(m).Run()
	ui.DrainStdin()
	if err != nil {
		return IngestCmdResult{}, err
	}

	fm := finalModel.(ingestModel)
	if fm.result == nil {
		return IngestCmdResult{}, fmt.Errorf("ingest did not complete")
	}
	if fm.result.err != nil {
		return IngestCmdResult{}, fm.result.err
	}

	result := IngestCmdResult{Stats: *fm.result.stats, DB: dbPath}
	result.Links.Resolved = fm.result.resolved
	result.Links.Broken = fm.result.broken
	return result, nil
}
