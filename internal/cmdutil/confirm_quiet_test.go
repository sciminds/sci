package cmdutil

// Confirm, ConfirmYes, and ConfirmOrSkip use huh.NewConfirm() for interactive
// prompts. This replaced the previous raw bufio.ReadString(os.Stdin) approach
// because bubbletea programs (spinners, progress bars) leave DECRQM terminal
// responses (modes 2026/2027) in the stdin buffer after exiting
// (charmbracelet/bubbletea#1590). Raw reads would pick up those escape
// sequences, making it impossible to type "y" — the answer would contain
// garbage like ^[[?2026;2$y prepended to the user's input.
//
// huh internally runs its own bubbletea program with proper raw-mode terminal
// handling, so it's immune to stale stdin bytes.
//
// Interactive huh forms can't be tested without a TTY, so these tests cover
// the quiet-mode (--json) auto-confirm path and the skip=true bypass.

import (
	"errors"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
)

func TestConfirm_QuietAutoConfirms(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	err := Confirm("Delete everything?")
	if err != nil {
		t.Errorf("Confirm in quiet mode should auto-confirm, got: %v", err)
	}
}

func TestConfirmYes_QuietAutoConfirms(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	err := ConfirmYes("Update Brewfile?")
	if err != nil {
		t.Errorf("ConfirmYes in quiet mode should auto-confirm, got: %v", err)
	}
}

func TestConfirmOrSkip_SkipTrue(t *testing.T) {
	// When skip=true (--yes flag), no prompt is shown regardless of
	// quiet mode. This is the most common path in automated/scripted usage.
	done, err := ConfirmOrSkip(true, "Drop table?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("ConfirmOrSkip with skip=true should return done=false")
	}
}

func TestConfirmRequired_QuietAutoConfirms(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	if err := ConfirmRequired("Install Homebrew?"); err != nil {
		t.Errorf("ConfirmRequired in quiet mode should auto-confirm, got: %v", err)
	}
}

func TestAssume_YesNo(t *testing.T) {
	t.Setenv("SCI_ASSUME", "yes")
	for name, fn := range map[string]func(string) error{
		"Confirm":         Confirm,
		"ConfirmYes":      ConfirmYes,
		"ConfirmRequired": ConfirmRequired,
	} {
		if err := fn("Proceed?"); err != nil {
			t.Errorf("%s with SCI_ASSUME=yes should succeed, got: %v", name, err)
		}
	}

	t.Setenv("SCI_ASSUME", "no")
	for name, fn := range map[string]func(string) error{
		"Confirm":         Confirm,
		"ConfirmYes":      ConfirmYes,
		"ConfirmRequired": ConfirmRequired,
	} {
		if err := fn("Proceed?"); !errors.Is(err, ErrCancelled) {
			t.Errorf("%s with SCI_ASSUME=no should return ErrCancelled, got: %v", name, err)
		}
	}
}

func TestConfirmOrSkip_QuietAutoConfirms(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	done, err := ConfirmOrSkip(false, "Drop table?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("ConfirmOrSkip in quiet mode should not cancel (done=false)")
	}
}
