package uikit

import (
	"testing"
)

func TestNewLineEditorEmpty(t *testing.T) {
	e := NewLineEditor("")
	if got := e.Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
	if e.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0", e.Cursor)
	}
}

func TestNewLineEditorPreFilled(t *testing.T) {
	e := NewLineEditor("hello")
	if got := e.Text(); got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}
	if e.Cursor != 5 {
		t.Errorf("Cursor = %d, want 5", e.Cursor)
	}
}

func TestLineEditorBackspace(t *testing.T) {
	t.Run("at zero is no-op", func(t *testing.T) {
		e := NewLineEditor("")
		e.Backspace()
		if got := e.Text(); got != "" {
			t.Errorf("Text() = %q after backspace at 0", got)
		}
	})
	t.Run("mid buffer", func(t *testing.T) {
		e := NewLineEditor("abc")
		e.Cursor = 2 // after 'b'
		e.Backspace()
		if got := e.Text(); got != "ac" {
			t.Errorf("Text() = %q, want %q", got, "ac")
		}
		if e.Cursor != 1 {
			t.Errorf("Cursor = %d, want 1", e.Cursor)
		}
	})
}

func TestLineEditorLeftRight(t *testing.T) {
	e := NewLineEditor("ab")

	e.Left()
	if e.Cursor != 1 {
		t.Errorf("after Left: Cursor = %d, want 1", e.Cursor)
	}

	e.Left()
	if e.Cursor != 0 {
		t.Errorf("after Left×2: Cursor = %d, want 0", e.Cursor)
	}

	// Clamp at 0
	e.Left()
	if e.Cursor != 0 {
		t.Errorf("Left past 0: Cursor = %d, want 0", e.Cursor)
	}

	e.Right()
	if e.Cursor != 1 {
		t.Errorf("after Right: Cursor = %d, want 1", e.Cursor)
	}

	e.Right()
	e.Right() // clamp at len
	if e.Cursor != 2 {
		t.Errorf("Right past end: Cursor = %d, want 2", e.Cursor)
	}
}

func TestLineEditorHomeEnd(t *testing.T) {
	e := NewLineEditor("hello")

	e.Home()
	if e.Cursor != 0 {
		t.Errorf("Home: Cursor = %d, want 0", e.Cursor)
	}

	e.End()
	if e.Cursor != 5 {
		t.Errorf("End: Cursor = %d, want 5", e.Cursor)
	}
}

func TestLineEditorInsertRunes(t *testing.T) {
	e := NewLineEditor("ac")
	e.Cursor = 1
	e.InsertRunes([]rune{'b'})
	if got := e.Text(); got != "abc" {
		t.Errorf("Text() = %q, want %q", got, "abc")
	}
	if e.Cursor != 2 {
		t.Errorf("Cursor = %d, want 2", e.Cursor)
	}
}

func TestLineEditorInsertFromKey(t *testing.T) {
	t.Run("with runes", func(t *testing.T) {
		e := NewLineEditor("")
		ok := e.InsertFromKey([]rune{'x'}, "")
		if !ok {
			t.Error("InsertFromKey returned false")
		}
		if got := e.Text(); got != "x" {
			t.Errorf("Text() = %q, want %q", got, "x")
		}
	})
	t.Run("fallback to key string", func(t *testing.T) {
		e := NewLineEditor("")
		ok := e.InsertFromKey(nil, "y")
		if !ok {
			t.Error("InsertFromKey returned false")
		}
		if got := e.Text(); got != "y" {
			t.Errorf("Text() = %q, want %q", got, "y")
		}
	})
	t.Run("empty returns false", func(t *testing.T) {
		e := NewLineEditor("")
		ok := e.InsertFromKey(nil, "")
		if ok {
			t.Error("InsertFromKey should return false for empty input")
		}
	})
}
