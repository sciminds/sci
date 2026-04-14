package uikit

import (
	"errors"
	"testing"

	"charm.land/huh/v2"
)

func TestRunForm_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("test"),
	))
	err := RunForm(form)
	if err == nil {
		t.Fatal("RunForm should return an error in quiet mode")
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

func TestErrFormAborted_MatchesHuh(t *testing.T) {
	// ErrFormAborted should be the same sentinel as huh.ErrUserAborted
	// so callers can use either for errors.Is checks.
	if !errors.Is(ErrFormAborted, huh.ErrUserAborted) {
		t.Error("ErrFormAborted should match huh.ErrUserAborted")
	}
}
