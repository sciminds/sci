package cmdutil

// Confirm, ConfirmYes, and ConfirmOrSkip render via uikit.Confirm for
// interactive prompts. This replaced the previous raw bufio.ReadString(os.Stdin)
// approach because bubbletea programs (spinners, progress bars) leave DECRQM
// terminal responses (modes 2026/2027) in the stdin buffer after exiting
// (charmbracelet/bubbletea#1590). Raw reads would pick up those escape
// sequences, making it impossible to type "y" — the answer would contain
// garbage like ^[[?2026;2$y prepended to the user's input.
//
// uikit's huh-backed prompt runs its own bubbletea program with proper raw-mode
// terminal handling, so it's immune to stale stdin bytes.
//
// Interactive huh forms can't be tested without a TTY, so these tests cover the
// two non-interactive paths: quiet mode (--json) and SCI_ASSUME.
//
// The contract they pin: **quiet mode is not consent.** --json turns quiet mode
// on, and a prompt that cannot be shown is not a prompt that was answered yes.
// Absent an explicit answer (a command's own --yes/--force flag, checked by the
// caller before it ever gets here, or SCI_ASSUME=yes), every entry point
// refuses with [ErrNeedsConsent] — a coded, exit-2 failure distinct from
// [ErrCancelled], so callers that map "the user said no" to a clean exit don't
// also swallow "nobody could be asked".

import (
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/uikit"
)

// quietNoAssume puts the process in quiet mode with SCI_ASSUME unset — the
// state a bare `sci … --json` runs in.
func quietNoAssume(t *testing.T) {
	t.Helper()
	t.Setenv("SCI_ASSUME", "")
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })
}

// assertNeedsConsent checks the refusal shape every quiet-mode prompt returns.
func assertNeedsConsent(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s in quiet mode returned nil — that is silent auto-consent", name)
	}
	if !errors.Is(err, ErrNeedsConsent) {
		t.Fatalf("%s should return ErrNeedsConsent, got: %v", name, err)
	}
	if errors.Is(err, ErrCancelled) {
		t.Fatalf("%s must not report ErrCancelled — callers map that to a clean exit 0", name)
	}
}

func TestConfirm_QuietRefusesConsent(t *testing.T) {
	quietNoAssume(t)
	assertNeedsConsent(t, "Confirm", Confirm("Delete everything?"))
}

func TestConfirmYes_QuietRefusesConsent(t *testing.T) {
	quietNoAssume(t)
	assertNeedsConsent(t, "ConfirmYes", ConfirmYes("Update Brewfile?"))
}

func TestConfirmRequired_QuietRefusesConsent(t *testing.T) {
	quietNoAssume(t)
	assertNeedsConsent(t, "ConfirmRequired", ConfirmRequired("Install Homebrew?"))
}

func TestConfirmOrSkip_QuietRefusesConsent(t *testing.T) {
	quietNoAssume(t)

	done, err := ConfirmOrSkip(false, "Drop table?")
	assertNeedsConsent(t, "ConfirmOrSkip", err)
	if !done {
		t.Error("ConfirmOrSkip should report done=true so the caller stops")
	}
}

func TestConfirmOrSkip_SkipTrue(t *testing.T) {
	// When skip=true (--yes flag), no prompt is shown regardless of
	// quiet mode. This is the consent path for automated/scripted usage.
	quietNoAssume(t)

	done, err := ConfirmOrSkip(true, "Drop table?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("ConfirmOrSkip with skip=true should return done=false")
	}
}

// TestNeedsConsent_Envelope pins how the refusal reaches an agent: a coded
// usage failure (exit 2 — "rewrite the command line and retry") whose remedy
// names the flag that grants consent, and whose message still quotes the
// question that went unanswered.
func TestNeedsConsent_Envelope(t *testing.T) {
	quietNoAssume(t)

	err := Confirm("Drop table \"grades\"?")
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("ErrNeedsConsent must unwrap to *CodedError so HandleError renders an envelope, got %T", err)
	}
	if coded.Code != CodeUsage {
		t.Errorf("code: got %q, want %q", coded.Code, CodeUsage)
	}
	if !strings.Contains(coded.Message, "Drop table") {
		t.Errorf("message should quote the prompt, got: %q", coded.Message)
	}
	if !strings.Contains(coded.Try, "--yes") {
		t.Errorf("try should point at --yes, got: %q", coded.Try)
	}
	// A Fix must be resubmittable verbatim; cmdutil never sees the command
	// line, so it can only offer prose guidance.
	if coded.Fix != "" {
		t.Errorf("Fix must stay empty — cmdutil cannot build a verbatim command, got: %q", coded.Fix)
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

// TestAssume_WinsOverQuiet is the ordering guarantee: SCI_ASSUME is consulted
// before the quiet-mode refusal, so an operator driving `--json` from a script
// can answer every prompt — including "no". The old code checked quiet first,
// which silently overrode an explicit SCI_ASSUME=no with "yes".
func TestAssume_WinsOverQuiet(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	t.Run("yes", func(t *testing.T) {
		t.Setenv("SCI_ASSUME", "yes")
		for name, fn := range map[string]func(string) error{
			"Confirm":         Confirm,
			"ConfirmYes":      ConfirmYes,
			"ConfirmRequired": ConfirmRequired,
		} {
			if err := fn("Proceed?"); err != nil {
				t.Errorf("%s: SCI_ASSUME=yes should consent under quiet mode, got: %v", name, err)
			}
		}
		if done, err := ConfirmOrSkip(false, "Proceed?"); done || err != nil {
			t.Errorf("ConfirmOrSkip: SCI_ASSUME=yes should consent, got done=%v err=%v", done, err)
		}
	})

	t.Run("no", func(t *testing.T) {
		t.Setenv("SCI_ASSUME", "no")
		for name, fn := range map[string]func(string) error{
			"Confirm":         Confirm,
			"ConfirmYes":      ConfirmYes,
			"ConfirmRequired": ConfirmRequired,
		} {
			err := fn("Proceed?")
			if !errors.Is(err, ErrCancelled) {
				t.Errorf("%s: SCI_ASSUME=no should refuse under quiet mode, got: %v", name, err)
			}
		}
		// ConfirmOrSkip turns a decline into a clean stop, not an error.
		done, err := ConfirmOrSkip(false, "Proceed?")
		if err != nil {
			t.Errorf("ConfirmOrSkip: SCI_ASSUME=no should exit cleanly, got: %v", err)
		}
		if !done {
			t.Error("ConfirmOrSkip: SCI_ASSUME=no should report done=true")
		}
	})
}
