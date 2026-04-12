// Package kit provides lightweight helpers that reduce Bubbletea boilerplate.
//
// The primitives here sit on top of Bubbletea — they don't replace it.
// They target the three most common friction points in TUI code:
//
//   - [Screen] — dispatch table that replaces repeated switch-on-screen
//     statements in View, Update, and key handlers.
//   - [Chrome] — title / body / status layout with automatic height math.
//   - [Grid2D] — reusable 2-D cursor with move, clamp, and wrap.
//
// All types are designed for unit testing without teatest (plain structs,
// no tea.Model dependency) and for integration testing with teatest
// (they compose naturally inside a Bubbletea Model).
package kit
