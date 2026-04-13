// Package ui is the design system for dbtui's terminal interface.
//
// It provides a dbtui-specific [Styles] singleton ([TUI]) built on the shared
// [uikit.Palette]. Overlay primitives, layout constants, icons, and key
// bindings come from [uikit] — this package only contains styles and helpers
// that are unique to the dbtui application.
//
// # Styles singleton
//
// The package-level variable [TUI] is a pre-built [Styles] instance.
// Access styles through its methods:
//
//	ui.TUI.TextBlue().Render("highlighted text")
//	ui.TUI.Error().Render("something went wrong")
//	ui.TUI.OverlayBox().Width(40).Render(content)
//
// Never create ad-hoc lipgloss styles in application code. If you need a new
// style, add it to the [Styles] struct with an accessor method.
package ui
