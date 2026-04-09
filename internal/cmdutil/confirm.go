package cmdutil

// confirm.go — interactive yes/no confirmation prompt used before destructive
// operations (drop table, overwrite file, etc.).

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// ErrCancelled is returned when the user declines a confirmation prompt.
var ErrCancelled = errors.New("cancelled")

// Confirm prompts the user with msg and waits for y/yes. Returns
// ErrCancelled if the user declines. The prompt is written to stderr
// so it doesn't pollute --json output.
// In quiet mode (--json), auto-confirms without prompting.
func Confirm(msg string) error {
	if ui.IsQuiet() {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s %s ", ui.TUI.Accent().Render(msg), ui.TUI.Dim().Render("[y/N]"))
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		return ErrCancelled
	}
	return nil
}

// ConfirmYes prompts with a default-yes [Y/n]. Empty input or "y"/"yes"
// returns nil. "n"/"no" returns ErrCancelled.
// In quiet mode (--json), auto-confirms without prompting.
func ConfirmYes(msg string) error {
	if ui.IsQuiet() {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s %s ", ui.TUI.Accent().Render(msg), ui.TUI.Dim().Render("[Y/n]"))
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
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
