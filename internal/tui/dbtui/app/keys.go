package app

// keys.go — string constants for all key bindings used across viewer modes.

const (
	// Navigation keys.
	keyUp        = "up"
	keyDown      = "down"
	keyLeft      = "left"
	keyRight     = "right"
	keyTab       = "tab"
	keyBackspace = "backspace"
	keyPgUp      = "pgup"
	keyPgDown    = "pgdown"
	keyShiftTab  = "shift+tab"

	// Action keys.
	keyEsc        = "esc"
	keyEnter      = "enter"
	keyShiftEnter = "shift+enter"

	// Modifier keys.
	keyCtrlC = "ctrl+c"
	keyCtrlD = "ctrl+d"
	keyCtrlN = "ctrl+n"
	keyCtrlP = "ctrl+p"
	keyCtrlQ = "ctrl+q"
	keyCtrlS = "ctrl+s"

	// Letters (lower).
	keyA = "a"
	keyC = "c"
	keyD = "d"
	keyE = "e"
	keyF = "f"
	keyG = "g"
	keyH = "h"
	keyI = "i"
	keyJ = "j"
	keyK = "k"
	keyL = "l"
	keyN = "n"
	keyQ = "q"
	keyS = "s"
	keyT = "t"
	keyU = "u"
	keyY = "y"

	keyP = "p"
	keyR = "r"
	keyV = "v"
	keyX = "x"

	// Letters (upper / shift).
	keyShiftC = "C"
	keyShiftD = "D"
	keyShiftG = "G"
	keyShiftJ = "J"
	keyShiftK = "K"
	keyShiftP = "P"
	keyShiftS = "S"
	keyShiftY = "Y"

	// Space.
	keySpace      = "space"
	keyShiftSpace = "shift+space"

	// Symbols.
	keyBang     = "!"
	keySlash    = "/"
	keyQuestion = "?"
	keyCaret    = "^"
	keyDollar   = "$"

	// Display symbols for key hints.
	symReturn = "\u21b5" // ↵
	symUp     = "\u2191" // ↑
	symDown   = "\u2193" // ↓
	symLeft   = "\u2190" // ←
	symRight  = "\u2192" // →

	// Triangles / cursors.
	symTriUp    = "\u25b2" // ▲
	symTriDown  = "\u25bc" // ▼
	symTriLeft  = "\u25c0" // ◀
	symTriRight = "\u25b6" // ▶

	// Text symbols.
	symEllipsis = "\u2026" // …
	symEmptySet = "\u2205" // ∅
	symEmDash   = "\u2014" // —
)
