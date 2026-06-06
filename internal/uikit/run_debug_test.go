package uikit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pingMsg is a distinctive message we can look for in the dump.
type pingMsg struct{ N int }

// pingModel emits a pingMsg on Init, then quits when it receives it — so a full
// run pushes a known message through the panicGuard's dump tap.
type pingModel struct{}

func (pingModel) Init() tea.Cmd { return func() tea.Msg { return pingMsg{N: 7} } }
func (pingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(pingMsg); ok {
		return pingModel{}, tea.Quit
	}
	return pingModel{}, nil
}
func (pingModel) View() tea.View { return tea.NewView("") }

// TestRunDumpsMessagesWhenEnvSet is the acceptance guard: with SCI_TUI_DEBUG
// pointed at a file, Run writes a tail-able, pretty-printed entry for every
// message. Not parallel — t.Setenv forbids it.
func TestRunDumpsMessagesWhenEnvSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.log")
	t.Setenv(TUIDebugEnv, path)

	if err := Run(pingModel{}, testOpts()...); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading dump %q: %v", path, err)
	}
	dump := string(data)

	for _, want := range []string{"pingMsg", "7", "#1"} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump missing %q\n--- dump ---\n%s", want, dump)
		}
	}
}

// TestRunNoDumpWhenEnvUnset proves the off state writes nothing. Not parallel —
// t.Setenv forbids it.
func TestRunNoDumpWhenEnvUnset(t *testing.T) {
	t.Setenv(TUIDebugEnv, "") // explicitly empty

	if d := newMsgDumper(); d != nil {
		d.close()
		t.Fatal("newMsgDumper() != nil with SCI_TUI_DEBUG empty")
	}

	// A full run with the var empty must not panic and must succeed.
	if err := Run(pingModel{}, testOpts()...); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

// TestNewMsgDumperOffInQuietMode keeps the dump out of the --json/quiet path.
// Not parallel — mutates the package-global quiet and uses t.Setenv.
func TestNewMsgDumperOffInQuietMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quiet.log")
	t.Setenv(TUIDebugEnv, path)
	SetQuiet(true)
	defer SetQuiet(false)

	if d := newMsgDumper(); d != nil {
		d.close()
		t.Fatal("newMsgDumper() != nil in quiet mode")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("quiet mode created %q (stat err = %v)", path, err)
	}
}
