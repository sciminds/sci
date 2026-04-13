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

// Confirm prompts the user with msg and waits for y/yes. Returns
// ErrCancelled if the user declines. Default is No.
// In quiet mode (--json), auto-confirms without prompting.
func Confirm(msg string) error {
	if uikit.IsQuiet() {
		return nil
	}
	confirmed := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(msg).
			Affirmative("Yes").
			Negative("No").
			Value(&confirmed),
	)).WithTheme(HuhTheme()).WithKeyMap(HuhKeyMap()).Run(); err != nil {
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
	confirmed := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(msg).
			Affirmative("Yes").
			Negative("No").
			Value(&confirmed),
	)).WithTheme(HuhTheme()).WithKeyMap(HuhKeyMap()).Run(); err != nil {
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
