package kit

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testOpts returns ProgramOptions that bypass the TTY requirement so
// tests can run in CI / non-interactive environments.
func testOpts() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
	}
}

// quitModel is a minimal tea.Model that quits immediately on Init.
type quitModel struct {
	value string
}

func (m quitModel) Init() tea.Cmd                       { return tea.Quit }
func (m quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m quitModel) View() tea.View                      { return tea.NewView(m.value) }

// ── Run ──────────────────────────────────────────────────────────────

func TestRunReturnsNilOnCleanExit(t *testing.T) {
	t.Parallel()
	err := Run(quitModel{value: "bye"}, testOpts()...)
	if err != nil {
		t.Errorf("Run() = %v, want nil", err)
	}
}

// ── RunModel ─────────────────────────────────────────────────────────

func TestRunModelReturnsFinalState(t *testing.T) {
	t.Parallel()
	initial := quitModel{value: "final-state"}
	final, err := RunModel(initial, testOpts()...)
	if err != nil {
		t.Fatalf("RunModel() error = %v", err)
	}
	if final.value != "final-state" {
		t.Errorf("final.value = %q, want %q", final.value, "final-state")
	}
}

func TestRunModelWorksWithPointerReceiver(t *testing.T) {
	t.Parallel()
	m := &ptrQuitModel{choice: 42}
	final, err := RunModel(m, testOpts()...)
	if err != nil {
		t.Fatalf("RunModel() error = %v", err)
	}
	if final.choice != 42 {
		t.Errorf("final.choice = %d, want 42", final.choice)
	}
}

// ptrQuitModel uses pointer receivers like most real TUI models.
type ptrQuitModel struct {
	choice int
}

func (m *ptrQuitModel) Init() tea.Cmd                       { return tea.Quit }
func (m *ptrQuitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *ptrQuitModel) View() tea.View                      { return tea.NewView("") }
