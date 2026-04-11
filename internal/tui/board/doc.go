// Package board is the bubbletea TUI for shared kanban boards synchronized
// via the internal/board headless engine.
//
// This root package holds only the thin Run entry point; the actual Bubble
// Tea model, views, and key handling live under the app/ subpackage, and
// lipgloss styles under the ui/ subpackage. The layout mirrors
// internal/tui/dbtui.
package board
