package uikit

// ui_form.go — huh form helpers: themed execution with automatic stdin drain.
//
// uikit owns huh entirely; no other package imports it. Callers reach for:
//   - single prompts: Input / InputInto / Select / MultiSelect
//   - multi-field forms: NewForm / FormGroup / FormInput / FormSelect, run via
//     Form.Run (the builder for several fields on one screen, with optional
//     conditional groups)
//   - yes/no confirmations: Confirm
// Every path funnels through runHuhForm so theme, keymap, and stdin drain are
// applied in exactly one place.

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/samber/lo"
)

// ErrFormQuiet is returned when a form would need interactive input but
// quiet mode (--json) is active. Callers should check for this and either
// provide a non-interactive fallback or return a usage error.
var ErrFormQuiet = errors.New("interactive form required but quiet mode is active")

// ErrFormAborted is re-exported from huh so callers can check for user
// cancellation without importing huh directly.
var ErrFormAborted = huh.ErrUserAborted

// runHuhForm applies the project theme and keymap, runs the form, and drains
// stdin afterward to absorb stale DECRQM terminal responses. This is the
// single, uikit-internal entry point for all huh form execution — every public
// helper (Input, Select, MultiSelect, Form.Run, Confirm) routes through it so
// callers never name huh.
//
// Returns ErrFormQuiet if quiet mode is active.
func runHuhForm(f *huh.Form) error {
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

// Option is a single Select/MultiSelect choice. It aliases huh.Option so
// callers build option lists with uikit.NewOption and never import huh
// directly — uikit owns all UI.
type Option[T comparable] = huh.Option[T]

// NewOption builds a Select/MultiSelect choice: label is shown to the user,
// value is returned when the option is picked.
func NewOption[T comparable](label string, value T) Option[T] {
	return huh.NewOption(label, value)
}

// InputOption configures an Input or InputInto prompt.
type InputOption func(*inputConfig)

type inputConfig struct {
	description string
	placeholder string
	echoMode    huh.EchoMode
	validate    func(string) error
}

// WithDescription sets the dimmed help line shown under a field's title. Input
// and InputInto take description as a positional argument; the multi-field
// builders (FormInput / FormSelect) take it through this option instead.
func WithDescription(s string) InputOption {
	return func(c *inputConfig) { c.description = s }
}

// WithPlaceholder sets greyed-out placeholder text inside the input.
func WithPlaceholder(s string) InputOption {
	return func(c *inputConfig) { c.placeholder = s }
}

// WithPassword masks the input (dots instead of characters), for secrets
// like API tokens. Keeps huh's EchoMode an uikit-internal detail so callers
// stay huh-free.
func WithPassword() InputOption {
	return func(c *inputConfig) { c.echoMode = huh.EchoModePassword }
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
	cfg.description = description // positional description wins over any option

	return runHuhForm(huh.NewForm(huh.NewGroup(newInputField(title, dst, cfg))))
}

// newInputField builds a themed huh.Input bound to dst from a resolved
// inputConfig. Shared by InputInto and the FormInput builder so the
// title/description/placeholder/echo/validate mapping lives in one place.
func newInputField(title string, dst *string, cfg inputConfig) *huh.Input {
	field := huh.NewInput().Title(title).Description(cfg.description).Value(dst)
	if cfg.placeholder != "" {
		field = field.Placeholder(cfg.placeholder)
	}
	if cfg.echoMode != 0 {
		field = field.EchoMode(cfg.echoMode)
	}
	if cfg.validate != nil {
		field = field.Validate(cfg.validate)
	}
	return field
}

// Select prompts the user to pick one option from a list. Returns the zero
// value and ErrFormAborted if the user cancels. Returns ErrFormQuiet in
// quiet mode.
func Select[T comparable](title string, options []Option[T]) (T, error) {
	var zero T
	if IsQuiet() {
		return zero, ErrFormQuiet
	}

	var selected T
	field := huh.NewSelect[T]().Title(title).Options(options...).Value(&selected)
	if err := runHuhForm(huh.NewForm(huh.NewGroup(field))); err != nil {
		return zero, err
	}
	return selected, nil
}

// MultiSelect prompts the user to tick zero or more options from a list
// (space toggles, enter confirms). Returns the selected values in option
// order — an empty slice if the user confirms without ticking anything.
// Returns nil and ErrFormAborted if the user cancels, or ErrFormQuiet in
// quiet mode. The list is filterable and height-capped so a long catalog
// scrolls within a fixed viewport instead of overflowing the screen.
func MultiSelect[T comparable](title, description string, options []Option[T]) ([]T, error) {
	if IsQuiet() {
		return nil, ErrFormQuiet
	}

	var selected []T
	field := huh.NewMultiSelect[T]().
		Title(title).
		Description(description).
		Options(options...).
		Filterable(true).
		Height(multiSelectHeight(len(options))).
		Value(&selected)
	if err := runHuhForm(huh.NewForm(huh.NewGroup(field))); err != nil {
		return nil, err
	}
	return selected, nil
}

// multiSelectHeight caps the total field height so a long option list scrolls
// inside a fixed viewport rather than pushing the prompt off-screen. Short
// lists size to their content; the +4 leaves room for the title, description,
// and help line huh renders around the options.
func multiSelectHeight(optionCount int) int {
	const maxHeight = 18
	if h := optionCount + 4; h < maxHeight {
		return h
	}
	return maxHeight
}

// ---------- multi-field form builder (FormInput, FormSelect, FormGroup) ----------
//
// For several fields on one screen (or several conditional screens) the single-
// prompt wrappers above don't fit. NewForm assembles FormGroup screens of
// FormInput / FormSelect fields and runs them through the same themed pipeline,
// so callers build multi-field forms without ever importing huh.

// Field is one prompt inside a multi-field [Form]. Build it with FormInput or
// FormSelect — never construct one directly. The build closure defers huh field
// construction until Form.Run, binding each field to its destination pointer at
// run time.
type Field struct {
	build func() huh.Field
}

// FormInput is the multi-field-form counterpart of InputInto: a single-line
// text prompt bound to *value, shown alongside the other fields in its group.
// It accepts the same options as Input — WithDescription, WithPlaceholder,
// WithPassword, WithValidation.
func FormInput(value *string, title string, opts ...InputOption) Field {
	var cfg inputConfig
	for _, o := range opts {
		o(&cfg)
	}
	return Field{build: func() huh.Field { return newInputField(title, value, cfg) }}
}

// FormSelect is the multi-field-form counterpart of Select: a single-choice
// picker bound to *value. Only WithDescription applies — placeholder, password,
// and validation are meaningless for a fixed option list.
func FormSelect[T comparable](value *T, title string, options []Option[T], opts ...InputOption) Field {
	var cfg inputConfig
	for _, o := range opts {
		o(&cfg)
	}
	return Field{build: func() huh.Field {
		return huh.NewSelect[T]().
			Title(title).
			Description(cfg.description).
			Options(options...).
			Value(value)
	}}
}

// Group is one screen of a [Form] — a set of fields shown together. Build it
// with FormGroup; make it conditional with HideWhen.
type Group struct {
	fields []Field
	hide   func() bool
}

// FormGroup bundles fields onto a single form screen.
func FormGroup(fields ...Field) Group {
	return Group{fields: fields}
}

// HideWhen makes the group disappear when cond returns true. cond is evaluated
// live as the user navigates, so it can read pointers bound by earlier fields —
// e.g. hide the package-manager screen once an earlier answer selects a
// writing-only project.
func (g Group) HideWhen(cond func() bool) Group {
	g.hide = cond
	return g
}

// Form is a multi-field, optionally multi-screen prompt — the sanctioned
// replacement for hand-built huh forms. Build with NewForm, run with Run.
type Form struct {
	groups []Group
}

// NewForm assembles groups into a runnable form. Each group is one screen,
// shown in order (skipping any whose HideWhen predicate is true at run time).
func NewForm(groups ...Group) *Form {
	return &Form{groups: groups}
}

// Run renders the form with the project theme and keymap, then drains stdin.
// Returns ErrFormQuiet in quiet mode and ErrFormAborted if the user cancels.
func (f *Form) Run() error {
	if IsQuiet() {
		return ErrFormQuiet
	}
	return runHuhForm(f.toHuh())
}

// toHuh lowers the uikit form into the huh form runHuhForm executes. Split out
// from Run so tests can assemble the huh form without a TTY.
func (f *Form) toHuh() *huh.Form {
	groups := lo.Map(f.groups, func(g Group, _ int) *huh.Group {
		fields := lo.Map(g.fields, func(fl Field, _ int) huh.Field { return fl.build() })
		hg := huh.NewGroup(fields...)
		if g.hide != nil {
			hg = hg.WithHideFunc(g.hide)
		}
		return hg
	})
	return huh.NewForm(groups...)
}

// Confirm renders a yes/no prompt and reports the user's choice. defaultYes
// seeds the highlighted button; affirmative and negative label the buttons.
// Returns ErrFormQuiet in quiet mode. This is the primitive behind
// cmdutil.Confirm — most callers want that policy wrapper (quiet auto-confirm,
// SCI_ASSUME handling, ErrCancelled) rather than this raw prompt.
func Confirm(title, affirmative, negative string, defaultYes bool) (bool, error) {
	if IsQuiet() {
		return false, ErrFormQuiet
	}
	confirmed := defaultYes
	field := huh.NewConfirm().
		Title(title).
		Affirmative(affirmative).
		Negative(negative).
		Value(&confirmed)
	if err := runHuhForm(huh.NewForm(huh.NewGroup(field))); err != nil {
		return false, err
	}
	return confirmed, nil
}
