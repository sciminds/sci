package uikit

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
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

// ── Command panic recovery ────────────────────────────────────────────

// panicCmdModel emits a command that panics on Init, exercising the
// runtime's panic guard (which should restore the terminal and surface
// the panic as ErrCommandPanic rather than wedging the terminal).
type panicCmdModel struct{}

func (m panicCmdModel) Init() tea.Cmd {
	return SafeCmd(func() tea.Msg { panic("cmd-boom") })
}
func (m panicCmdModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m panicCmdModel) View() tea.View                      { return tea.NewView("") }

func TestRunSurfacesCommandPanic(t *testing.T) {
	t.Parallel()
	err := Run(panicCmdModel{}, testOpts()...)
	if !errors.Is(err, ErrCommandPanic) {
		t.Fatalf("Run() = %v, want ErrCommandPanic", err)
	}
	if !strings.Contains(err.Error(), "cmd-boom") {
		t.Errorf("error %q does not mention the panic value", err)
	}
}

func TestRunModelSurfacesCommandPanic(t *testing.T) {
	t.Parallel()
	_, err := RunModel(panicCmdModel{}, testOpts()...)
	if !errors.Is(err, ErrCommandPanic) {
		t.Fatalf("RunModel() = %v, want ErrCommandPanic", err)
	}
}

// TestPanicGuardQuitsUnderTeatest exercises the guard through the teatest
// harness, which drives the model directly and bypasses [Run]. It proves a
// panicking command makes the guard quit on its own (capturing the panic)
// instead of hanging or crashing the test binary.
func TestPanicGuardQuitsUnderTeatest(t *testing.T) {
	t.Parallel()
	tm := teatest.NewTestModel(t,
		panicGuard{inner: panicCmdModel{}},
		teatest.WithInitialTermSize(40, 10),
	)
	t.Cleanup(func() { _ = tm.Quit() })

	fm := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(panicGuard)
	if fm.captured == nil {
		t.Fatal("guard did not capture the command panic")
	}
	if fm.captured.Value != "cmd-boom" {
		t.Errorf("captured.Value = %v, want cmd-boom", fm.captured.Value)
	}
}
