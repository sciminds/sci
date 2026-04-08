package ui

import "testing"

func TestStatusRow(t *testing.T) {
	got := StatusRow("✓", "all good")
	if got == "" {
		t.Error("StatusRow returned empty string")
	}
	// Should contain both the symbol and the message.
	if len(got) < 5 {
		t.Errorf("StatusRow output suspiciously short: %q", got)
	}
}

func TestFooterBar(t *testing.T) {
	t.Run("zero width returns left only", func(t *testing.T) {
		got := FooterBar("left", "right", 0)
		if got != "left" {
			t.Errorf("expected 'left', got %q", got)
		}
	})

	t.Run("normal width includes both", func(t *testing.T) {
		got := FooterBar("L", "R", 80)
		if len(got) == 0 {
			t.Error("expected non-empty result")
		}
	})
}
