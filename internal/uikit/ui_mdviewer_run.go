package uikit

import tea "charm.land/bubbletea/v2"

// mdProgram is a tea.Model that wraps MdViewer with title/status chrome and
// owns top-level keys (q/esc/ctrl+c quit). Quit keys are gated on
// MdViewer.Searching so a literal 'q' typed into an open search query is
// passed through to the input instead of terminating the program.
type mdProgram struct {
	viewer   *MdViewer
	name     string
	w, h     int
	quitting bool
}

func newMdProgram(name, markdown string) *mdProgram {
	v := NewMdViewer(name, markdown)
	v.SetExtraHints([]string{"q quit"})
	return &mdProgram{viewer: v, name: name}
}

func (m *mdProgram) Init() tea.Cmd { return nil }

func (m *mdProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		// ctrl+c always quits. q/esc are gated on search mode so a literal
		// 'q' typed into an open query is passed through, and esc routes to
		// the viewer (where it clears the search) instead of killing the app.
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "q", "esc":
			if !m.viewer.Searching() {
				m.quitting = true
				return m, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m *mdProgram) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	body := Chrome{
		Title:  func(_ int) string { return TUI.Title().Render(m.name) },
		Status: func(w int) string { return m.viewer.Footer(w) },
		Body: func(w, h int) string {
			// MdViewer.View appends a search input line below the viewport
			// when searching, so reserve 1 row to keep the body exactly h
			// lines tall and preserve Chrome's height invariant.
			vh := h
			if m.viewer.Searching() {
				vh = h - 1
			}
			m.viewer.SetSize(w, vh)
			return m.viewer.View()
		},
	}.Render(m.w, m.h)
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

// RunMdViewer launches a full-screen markdown viewer for the given content.
// Detects terminal style before bubbletea takes over stdin so glamour picks
// the right palette. Used by `sci view <file.md>` and any other CLI surface
// that needs a one-off "show this document" runner.
func RunMdViewer(name, markdown string) error {
	DetectTermStyle()
	return Run(newMdProgram(name, markdown))
}
