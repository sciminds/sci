package uikit

import (
	"slices"
	"unicode/utf8"
)

// LineEditor is a reusable single-line rune buffer with cursor management.
// Used by overlay text inputs that need simpler editing than a full textarea.
type LineEditor struct {
	Buf    []rune
	Cursor int
}

// NewLineEditor creates a LineEditor pre-filled with text and the cursor at the end.
func NewLineEditor(text string) LineEditor {
	runes := []rune(text)
	return LineEditor{Buf: runes, Cursor: len(runes)}
}

// Text returns the current buffer contents as a string.
func (e *LineEditor) Text() string {
	return string(e.Buf)
}

// Backspace deletes the rune before the cursor.
func (e *LineEditor) Backspace() {
	if e.Cursor > 0 {
		e.Buf = append(e.Buf[:e.Cursor-1], e.Buf[e.Cursor:]...)
		e.Cursor--
	}
}

// Left moves the cursor one position to the left.
func (e *LineEditor) Left() {
	if e.Cursor > 0 {
		e.Cursor--
	}
}

// Right moves the cursor one position to the right.
func (e *LineEditor) Right() {
	if e.Cursor < len(e.Buf) {
		e.Cursor++
	}
}

// Home moves the cursor to the beginning of the buffer.
func (e *LineEditor) Home() {
	e.Cursor = 0
}

// End moves the cursor to the end of the buffer.
func (e *LineEditor) End() {
	e.Cursor = len(e.Buf)
}

// InsertRunes inserts one or more runes at the cursor position.
func (e *LineEditor) InsertRunes(runes []rune) {
	for _, rn := range runes {
		e.Buf = slices.Insert(e.Buf, e.Cursor, rn)
		e.Cursor++
	}
}

// InsertFromKey extracts runes from key.Runes or falls back to the key string,
// then inserts them. Returns true if anything was inserted.
func (e *LineEditor) InsertFromKey(runes []rune, keyStr string) bool {
	r := runes
	if len(r) == 0 {
		if utf8.RuneCountInString(keyStr) == 1 {
			rn, _ := utf8.DecodeRuneInString(keyStr)
			r = []rune{rn}
		}
	}
	if len(r) == 0 {
		return false
	}
	e.InsertRunes(r)
	return true
}
