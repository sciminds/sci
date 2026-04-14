package app

import "testing"

func TestModeTUISingletonNotNil(t *testing.T) {
	if modeTUI == nil {
		t.Fatal("modeTUI singleton is nil")
	}
}

func TestModeStyleAccessorsNonZero(t *testing.T) {
	// Each accessor should render with a visible effect (background color)
	// so the output differs from plain text.
	accessors := []struct {
		name string
		fn   func() string
	}{
		{"CursorRaised", func() string { return modeTUI.CursorRaised().Render("x") }},
		{"CursorBlue", func() string { return modeTUI.CursorBlue().Render("x") }},
		{"CursorOrange", func() string { return modeTUI.CursorOrange().Render("x") }},
		{"CursorPink", func() string { return modeTUI.CursorPink().Render("x") }},
		{"SelectPink", func() string { return modeTUI.SelectPink().Render("x") }},
		{"HeaderGreenBg", func() string { return modeTUI.HeaderGreenBg().Render("x") }},
	}
	for _, tt := range accessors {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got == "x" {
				t.Errorf("%s().Render(\"x\") returned plain \"x\" — style has no effect", tt.name)
			}
		})
	}
}
