package cmdutil

import (
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestConfirm_QuietAutoConfirms(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

	err := Confirm("Delete everything?")
	if err != nil {
		t.Errorf("Confirm in quiet mode should auto-confirm, got: %v", err)
	}
}

func TestConfirmYes_QuietAutoConfirms(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

	err := ConfirmYes("Update Brewfile?")
	if err != nil {
		t.Errorf("ConfirmYes in quiet mode should auto-confirm, got: %v", err)
	}
}

func TestConfirmOrSkip_QuietAutoConfirms(t *testing.T) {
	ui.SetQuiet(true)
	defer ui.SetQuiet(false)

	done, err := ConfirmOrSkip(false, "Drop table?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("ConfirmOrSkip in quiet mode should not cancel (done=false)")
	}
}
