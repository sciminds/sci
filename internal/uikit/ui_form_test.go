package uikit

import (
	"errors"
	"testing"

	"charm.land/huh/v2"
)

func TestRunHuhForm_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("test"),
	))
	err := runHuhForm(form)
	if err == nil {
		t.Fatal("runHuhForm should return an error in quiet mode")
	}
	if !errors.Is(err, ErrFormQuiet) {
		t.Errorf("expected ErrFormQuiet, got: %v", err)
	}
}

func TestInput_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	_, err := Input("Name", "enter your name")
	if !errors.Is(err, ErrFormQuiet) {
		t.Errorf("Input in quiet mode should return ErrFormQuiet, got: %v", err)
	}
}

func TestInputInto_QuietKeepsDefault(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	value := "existing"
	err := InputInto(&value, "Name", "enter your name")
	if err != nil {
		t.Fatalf("InputInto with default in quiet mode should succeed, got: %v", err)
	}
	if value != "existing" {
		t.Errorf("expected value to stay %q, got %q", "existing", value)
	}
}

func TestInputInto_QuietNoDefaultReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	var value string
	err := InputInto(&value, "Name", "enter your name")
	if !errors.Is(err, ErrFormQuiet) {
		t.Errorf("InputInto without default in quiet mode should return ErrFormQuiet, got: %v", err)
	}
}

func TestSelect_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	opts := []huh.Option[string]{
		huh.NewOption("A", "a"),
		huh.NewOption("B", "b"),
	}
	_, err := Select("Pick one", opts)
	if !errors.Is(err, ErrFormQuiet) {
		t.Errorf("Select in quiet mode should return ErrFormQuiet, got: %v", err)
	}
}

func TestMultiSelect_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	opts := []huh.Option[string]{
		huh.NewOption("A", "a"),
		huh.NewOption("B", "b"),
	}
	got, err := MultiSelect("Pick some", "tick what you want", opts)
	if !errors.Is(err, ErrFormQuiet) {
		t.Errorf("MultiSelect in quiet mode should return ErrFormQuiet, got: %v", err)
	}
	if got != nil {
		t.Errorf("MultiSelect in quiet mode should return nil selection, got: %v", got)
	}
}

func TestMultiSelectHeight_CapsLongLists(t *testing.T) {
	// Short lists size to content (+4 chrome); long lists cap at the ceiling.
	if h := multiSelectHeight(3); h != 7 {
		t.Errorf("multiSelectHeight(3) = %d, want 7 (3 + 4 chrome)", h)
	}
	if h := multiSelectHeight(50); h != 18 {
		t.Errorf("multiSelectHeight(50) = %d, want 18 (capped)", h)
	}
}

func TestErrFormAborted_MatchesHuh(t *testing.T) {
	// ErrFormAborted should be the same sentinel as huh.ErrUserAborted
	// so callers can use either for errors.Is checks.
	if !errors.Is(ErrFormAborted, huh.ErrUserAborted) {
		t.Error("ErrFormAborted should match huh.ErrUserAborted")
	}
}

func TestNewOption_SetsKeyAndValue(t *testing.T) {
	// NewOption lets callers build Select/MultiSelect options without
	// importing huh themselves (uikit owns all UI).
	opt := NewOption("Label", 42)
	if opt.Key != "Label" {
		t.Errorf("NewOption key = %q, want %q", opt.Key, "Label")
	}
	if opt.Value != 42 {
		t.Errorf("NewOption value = %d, want 42", opt.Value)
	}
}

func TestWithPassword_SetsPasswordEcho(t *testing.T) {
	// WithPassword is the huh-free way to mask an input — callers no longer
	// reach for huh.EchoModePassword.
	var cfg inputConfig
	WithPassword()(&cfg)
	if cfg.echoMode != huh.EchoModePassword {
		t.Errorf("WithPassword echoMode = %v, want %v", cfg.echoMode, huh.EchoModePassword)
	}
}

func TestWithDescription_SetsDescription(t *testing.T) {
	// WithDescription feeds the multi-field builders, which take description as
	// an option rather than a positional arg.
	var cfg inputConfig
	WithDescription("help line")(&cfg)
	if cfg.description != "help line" {
		t.Errorf("WithDescription description = %q, want %q", cfg.description, "help line")
	}
}

// ---------- multi-field form builder ----------

func TestForm_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	var name string
	form := NewForm(FormGroup(FormInput(&name, "Name")))
	if err := form.Run(); !errors.Is(err, ErrFormQuiet) {
		t.Errorf("Form.Run in quiet mode should return ErrFormQuiet, got: %v", err)
	}
}

func TestConfirm_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	if _, err := Confirm("Proceed?", "Yes", "No", true); !errors.Is(err, ErrFormQuiet) {
		t.Errorf("Confirm in quiet mode should return ErrFormQuiet, got: %v", err)
	}
}

func TestNewForm_PreservesGroupAndFieldOrder(t *testing.T) {
	var a, b, c string
	form := NewForm(
		FormGroup(FormInput(&a, "A"), FormInput(&b, "B")),
		FormGroup(FormInput(&c, "C")),
	)
	if len(form.groups) != 2 {
		t.Fatalf("NewForm groups = %d, want 2", len(form.groups))
	}
	if len(form.groups[0].fields) != 2 {
		t.Errorf("group 0 fields = %d, want 2", len(form.groups[0].fields))
	}
	if len(form.groups[1].fields) != 1 {
		t.Errorf("group 1 fields = %d, want 1", len(form.groups[1].fields))
	}
}

func TestHideWhen_StoresPredicate(t *testing.T) {
	// HideWhen wires a live predicate so conditional groups (e.g. wizard.go's
	// package-manager screen) can react to earlier answers.
	hidden := true
	var x string
	g := FormGroup(FormInput(&x, "X")).HideWhen(func() bool { return hidden })
	if g.hide == nil {
		t.Fatal("HideWhen should store a predicate")
	}
	if !g.hide() {
		t.Error("predicate should report hidden=true")
	}
	hidden = false
	if g.hide() {
		t.Error("predicate should track the captured variable (now false)")
	}
}

func TestFormFields_BuildExpectedHuhTypes(t *testing.T) {
	// The build closures must produce the right huh field type so toHuh
	// assembles a real form. FormInput → *huh.Input, FormSelect → *huh.Select.
	var text string
	if _, ok := FormInput(&text, "Name").build().(*huh.Input); !ok {
		t.Error("FormInput should build a *huh.Input")
	}

	var choice string
	field := FormSelect(&choice, "Pick", []Option[string]{NewOption("A", "a")})
	if _, ok := field.build().(*huh.Select[string]); !ok {
		t.Error("FormSelect should build a *huh.Select[string]")
	}
}

func TestForm_ToHuhAssemblesAllGroups(t *testing.T) {
	// toHuh must not panic and must yield a runnable huh form covering every
	// group, including a conditional one.
	var a, b string
	form := NewForm(
		FormGroup(FormInput(&a, "A", WithValidation(func(string) error { return nil }))),
		FormGroup(FormSelect(&b, "B", []Option[string]{NewOption("X", "x")})).
			HideWhen(func() bool { return false }),
	)
	if got := form.toHuh(); got == nil {
		t.Fatal("toHuh returned nil")
	}
}
