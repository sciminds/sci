package cmdutil

// confirm.go — interactive yes/no confirmation prompt used before destructive
// operations (drop table, overwrite file, etc.).

import (
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/sci/internal/uikit"
)

// ErrCancelled is returned when the user declines a confirmation prompt.
var ErrCancelled = errors.New("cancelled")

// ErrNeedsConsent is returned when a confirmation is required but no answer
// can be obtained: quiet mode (--json) is on, no SCI_ASSUME is set, and the
// caller passed no explicit consent flag.
//
// It is deliberately distinct from [ErrCancelled]. Several call sites map a
// decline to "print cancelled, return nil, exit 0"; returning ErrCancelled
// here would turn `sci vid convert --json` into a silent no-op that reports
// success. "The user said no" and "nobody could be asked" need different
// answers, so they get different errors.
//
// Match it with errors.Is. The concrete value unwraps to a [CodedError]
// ([CodeUsage], exit 2) carrying the unanswered prompt and a Try that names
// the flag which grants consent, so [HandleError] renders it as an ordinary
// failure envelope.
var ErrNeedsConsent = errors.New("confirmation required but no answer is available")

// consentError is the value returned in place of an unanswerable prompt. It
// keeps the coded envelope (for HandleError, via Unwrap) and the sentinel
// identity (for errors.Is, via Is) in one error.
type consentError struct {
	*CodedError
}

// Is reports ErrNeedsConsent identity so callers can branch with errors.Is
// without depending on this unexported type.
func (e *consentError) Is(target error) bool { return target == ErrNeedsConsent }

// Unwrap exposes the coded envelope so errors.As/[HandleError] find it.
func (e *consentError) Unwrap() error { return e.CodedError }

// needsConsent builds the refusal for a prompt that cannot be shown.
//
// The code is [CodeUsage] (exit 2 — "rewrite the command line and retry")
// because that is precisely the remedy: add the command's consent flag. The
// remedy rides in Try rather than Fix: a Fix must be resubmittable verbatim,
// and cmdutil never sees the command line, only the question.
func needsConsent(msg string) error {
	return &consentError{
		CodedError: Coded(CodeUsage, "%s — refusing to answer for you in non-interactive mode", msg).
			WithTry("re-run with --yes (some commands spell it --force) to confirm, or set SCI_ASSUME=yes to answer every prompt"),
	}
}

// assumeAnswer lets CI and automated tests short-circuit every confirmation
// prompt without a TTY. Values: "yes" → nil, "no" → ErrCancelled.
// Any other value is ignored. Intended for CI use — prefer explicit flags.
//
// It is consulted *before* the quiet-mode refusal, so an operator driving
// --json from a script can still answer — including "no".
func assumeAnswer() (handled bool, err error) {
	switch os.Getenv("SCI_ASSUME") {
	case "yes":
		return true, nil
	case "no":
		return true, ErrCancelled
	}
	return false, nil
}

// Confirm prompts the user with msg and waits for y/yes. Returns
// ErrCancelled if the user declines. Default is No.
// In quiet mode (--json) it returns [ErrNeedsConsent] — see that doc for why
// non-interactive is not consent.
func Confirm(msg string) error {
	if handled, err := assumeAnswer(); handled {
		return err
	}
	if uikit.IsQuiet() {
		return needsConsent(msg)
	}
	return runConfirm(msg, "Yes", "No", false)
}

// ConfirmYes prompts with a default-yes. Empty input or "y"/"yes"
// returns nil. "n"/"no" returns ErrCancelled.
// In quiet mode (--json) it returns [ErrNeedsConsent]: a default-yes prompt is
// still a prompt, and the default only applies to a person who saw it.
func ConfirmYes(msg string) error {
	if handled, err := assumeAnswer(); handled {
		return err
	}
	if uikit.IsQuiet() {
		return needsConsent(msg)
	}
	return runConfirm(msg, "Yes", "No", true)
}

// ConfirmRequired prompts with an emphasized "Yes (required)" affirmative
// label for prerequisites the user almost certainly wants. Default is yes.
// In quiet mode (--json) it returns [ErrNeedsConsent].
func ConfirmRequired(msg string) error {
	if handled, err := assumeAnswer(); handled {
		return err
	}
	if uikit.IsQuiet() {
		return needsConsent(msg)
	}
	return runConfirm(msg, "Yes (required)", "No", true)
}

// runConfirm renders the yes/no prompt via uikit (uikit owns huh) and maps a
// "No" answer to ErrCancelled. Quiet-mode and SCI_ASSUME short-circuits are
// handled by the callers before they reach here.
func runConfirm(msg, affirmative, negative string, defaultYes bool) error {
	confirmed, err := uikit.Confirm(msg, affirmative, negative, defaultYes)
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return nil
}

// ConfirmOrSkip prompts unless skip is true. On decline, prints "cancelled"
// to stderr and returns nil (so the calling command exits cleanly).
// Returns a non-nil error for unexpected I/O failures and for
// [ErrNeedsConsent] — a prompt nobody could answer is a failure, not a
// cancellation, so the command reports it instead of exiting 0 having done
// nothing.
//
// skip is the caller's consent flag (`--yes`, `--force`). Every command that
// can reach a prompt under --json needs one, or the prompt becomes an
// unanswerable dead end.
//
// Usage: if done, err := cmdutil.ConfirmOrSkip(skip, msg); done || err != nil { return err }
func ConfirmOrSkip(skip bool, msg string) (done bool, err error) {
	if skip {
		return false, nil
	}
	if err := Confirm(msg); err != nil {
		if errors.Is(err, ErrCancelled) {
			fmt.Fprintln(os.Stderr, "cancelled")
			return true, nil
		}
		return true, err
	}
	return false, nil
}
