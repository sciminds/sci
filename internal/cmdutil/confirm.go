package cmdutil

// confirm.go — interactive yes/no confirmation prompt used before destructive
// operations (drop table, overwrite file, etc.).

import (
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// ErrCancelled is returned when the user declines a confirmation prompt.
var ErrCancelled = errors.New("cancelled")

// assumeAnswer lets CI and automated tests short-circuit every confirmation
// prompt without a TTY. Values: "yes" → nil, "no" → ErrCancelled.
// Any other value is ignored. Intended for CI use — prefer explicit flags.
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
// In quiet mode (--json), auto-confirms without prompting.
func Confirm(msg string) error {
	if uikit.IsQuiet() {
		return nil
	}
	if handled, err := assumeAnswer(); handled {
		return err
	}
	confirmed := false
	if err := uikit.RunForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(msg).
			Affirmative("Yes").
			Negative("No").
			Value(&confirmed),
	))); err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return nil
}

// ConfirmYes prompts with a default-yes. Empty input or "y"/"yes"
// returns nil. "n"/"no" returns ErrCancelled.
// In quiet mode (--json), auto-confirms without prompting.
func ConfirmYes(msg string) error {
	if uikit.IsQuiet() {
		return nil
	}
	if handled, err := assumeAnswer(); handled {
		return err
	}
	confirmed := true
	if err := uikit.RunForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(msg).
			Affirmative("Yes").
			Negative("No").
			Value(&confirmed),
	))); err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return nil
}

// ConfirmRequired prompts with an emphasized "Yes (required)" affirmative
// label for prerequisites the user almost certainly wants. Default is yes.
// In quiet mode (--json), auto-confirms without prompting.
func ConfirmRequired(msg string) error {
	if uikit.IsQuiet() {
		return nil
	}
	if handled, err := assumeAnswer(); handled {
		return err
	}
	confirmed := true
	if err := uikit.RunForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(msg).
			Affirmative("Yes (required)").
			Negative("No").
			Value(&confirmed),
	))); err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return nil
}

// ConfirmOrSkip prompts unless skip is true. On decline, prints "cancelled"
// to stderr and returns nil (so the calling command exits cleanly).
// Returns a non-nil error only for unexpected I/O failures.
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
