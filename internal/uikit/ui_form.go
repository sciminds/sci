package uikit

// ui_form.go — huh form helpers: themed execution with automatic stdin drain.
// All huh form execution should go through RunForm (or the convenience
// wrappers Input, InputInto, Select) so callers never need to remember
// theme, keymap, or drain boilerplate.

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// ErrFormQuiet is returned when a form would need interactive input but
// quiet mode (--json) is active. Callers should check for this and either
// provide a non-interactive fallback or return a usage error.
var ErrFormQuiet = errors.New("interactive form required but quiet mode is active")

// ErrFormAborted is re-exported from huh so callers can check for user
// cancellation without importing huh directly.
var ErrFormAborted = huh.ErrUserAborted

// RunForm applies the project theme and keymap, runs the form, and drains
// stdin afterward to absorb stale DECRQM terminal responses. This is the
// single entry point for all huh form execution.
//
// Returns ErrFormQuiet if quiet mode is active.
func RunForm(f *huh.Form) error {
	if IsQuiet() {
		return ErrFormQuiet
	}
	f = f.WithTheme(HuhTheme()).WithKeyMap(HuhKeyMap())
	err := f.Run()
	DrainStdin()
	return err
}

// HuhTheme returns a huh.ThemeFunc built from the project's Wong
// colorblind-safe palette.
func HuhTheme() huh.ThemeFunc {
	return func(isDark bool) *huh.Styles {
		p := TUI.Palette()
		t := huh.ThemeBase(isDark)

		f := &t.Focused
		f.Base = f.Base.BorderForeground(p.Border)
		f.Card = f.Base
		f.Title = f.Title.Foreground(p.Blue).Bold(true)
		f.NoteTitle = f.NoteTitle.Foreground(p.Blue).Bold(true)
		f.Description = f.Description.Foreground(p.TextMid)
		f.ErrorIndicator = f.ErrorIndicator.Foreground(p.Red)
		f.ErrorMessage = f.ErrorMessage.Foreground(p.Red)

		// Select
		f.SelectSelector = f.SelectSelector.Foreground(p.Orange)
		f.NextIndicator = f.NextIndicator.Foreground(p.Orange)
		f.PrevIndicator = f.PrevIndicator.Foreground(p.Orange)
		f.Option = f.Option.Foreground(p.TextBright)
		f.SelectedOption = f.SelectedOption.Foreground(p.Green)
		f.SelectedPrefix = lipgloss.NewStyle().Foreground(p.Green).SetString("✓ ")
		f.UnselectedPrefix = lipgloss.NewStyle().Foreground(p.TextMid).SetString("• ")
		f.UnselectedOption = f.UnselectedOption.Foreground(p.TextBright)

		// Multi-select
		f.MultiSelectSelector = f.MultiSelectSelector.Foreground(p.Orange)

		// Buttons
		f.FocusedButton = f.FocusedButton.Foreground(p.OnAccent).Background(p.Blue)
		f.Next = f.FocusedButton
		f.BlurredButton = f.BlurredButton.Foreground(p.TextMid).Background(p.Surface)

		// Text input
		f.TextInput.Cursor = f.TextInput.Cursor.Foreground(p.Blue)
		f.TextInput.Placeholder = f.TextInput.Placeholder.Foreground(p.TextDim)
		f.TextInput.Prompt = f.TextInput.Prompt.Foreground(p.Orange)
		f.TextInput.Text = f.TextInput.Text.Foreground(p.TextBright)

		// Blurred: same styles but hidden border
		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()

		// Group
		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description

		return t
	}
}

// HuhKeyMap returns a huh.KeyMap with esc and q added to the Quit binding
// so users can cancel forms with ctrl+c, esc, or q, and tab/shift+tab
// added to the Confirm toggle so users can flip yes/no with tab.
func HuhKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc", "q"))
	km.Confirm.Toggle = key.NewBinding(
		key.WithKeys("h", "l", "right", "left", "tab", "shift+tab"),
		key.WithHelp("←/→/tab", "toggle"),
	)
	return km
}

// ---------- convenience wrappers (Input, InputInto, Select) ----------

// InputOption configures an Input or InputInto prompt.
type InputOption func(*inputConfig)

type inputConfig struct {
	placeholder string
	echoMode    huh.EchoMode
	validate    func(string) error
}

// WithPlaceholder sets greyed-out placeholder text inside the input.
func WithPlaceholder(s string) InputOption {
	return func(c *inputConfig) { c.placeholder = s }
}

// WithEchoMode sets the echo mode (e.g. huh.EchoModePassword).
func WithEchoMode(m huh.EchoMode) InputOption {
	return func(c *inputConfig) { c.echoMode = m }
}

// WithValidation attaches a validation function to the input.
func WithValidation(fn func(string) error) InputOption {
	return func(c *inputConfig) { c.validate = fn }
}

// Input prompts for a single text value. Returns ("", ErrFormAborted) if
// the user cancels. Returns ("", ErrFormQuiet) in quiet mode.
func Input(title, description string, opts ...InputOption) (string, error) {
	var value string
	if err := InputInto(&value, title, description, opts...); err != nil {
		return "", err
	}
	return value, nil
}

// InputInto prompts for a single text value, writing the result into *dst.
// If *dst is non-empty it appears as the pre-filled default. In quiet mode,
// if *dst is already non-empty the call succeeds (keeps the default);
// otherwise it returns ErrFormQuiet.
func InputInto(dst *string, title, description string, opts ...InputOption) error {
	if IsQuiet() {
		if *dst != "" {
			return nil // keep existing default
		}
		return fmt.Errorf("%s: %w", title, ErrFormQuiet)
	}

	var cfg inputConfig
	for _, o := range opts {
		o(&cfg)
	}

	field := huh.NewInput().Title(title).Description(description).Value(dst)
	if cfg.placeholder != "" {
		field = field.Placeholder(cfg.placeholder)
	}
	if cfg.echoMode != 0 {
		field = field.EchoMode(cfg.echoMode)
	}
	if cfg.validate != nil {
		field = field.Validate(cfg.validate)
	}

	return RunForm(huh.NewForm(huh.NewGroup(field)))
}

// Select prompts the user to pick one option from a list. Returns the zero
// value and ErrFormAborted if the user cancels. Returns ErrFormQuiet in
// quiet mode.
func Select[T comparable](title string, options []huh.Option[T]) (T, error) {
	var zero T
	if IsQuiet() {
		return zero, ErrFormQuiet
	}

	var selected T
	field := huh.NewSelect[T]().Title(title).Options(options...).Value(&selected)
	if err := RunForm(huh.NewForm(huh.NewGroup(field))); err != nil {
		return zero, err
	}
	return selected, nil
}
