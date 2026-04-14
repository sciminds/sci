// Package uikit provides the shared visual foundation for all TUI and CLI
// output in the project. It is dependency-free of project-specific packages
// (no pocketbase, no urfave/cli, no internal/db) so standalone binaries
// (dbtui, zot) can import it without pulling in the full CLI dependency tree.
//
// The package is organized into logical layers, reflected by file prefixes:
//
// # Colors (color_palette.go, color_styles.go, color_symbols.go)
//
// [Palette] holds the Wong colorblind-safe color set, resolved for light/dark
// terminals at init. [Styles] wraps ~70 pre-built lipgloss styles accessed
// via the package-level [TUI] singleton. Icon constants (✓, ✗, ⚠, →),
// pre-rendered symbols (SymOK, SymFail, SymWarn, SymArrow), and printf-style
// status helpers (OK, Hint, Header, NextStep) live in color_symbols.go.
//
// # Input (input_keys.go, input_keymap.go)
//
// Key string constants replace bare literals in Bubbletea switch cases.
// Shared key bindings (BindQuit, BindUp, BindDown, BindEnter, BindHelp)
// are composed into per-TUI KeyMaps.
//
// # Layout (layout_dims.go, layout_compose.go)
//
// Dimension constants, clamping helpers, and declarative layout composition
// utilities (Spread, Center, Pad, Fit, FitHeight, WordWrap, PageLayout,
// SummaryLine, FooterBar, StatusRow) for building terminal UIs.
//
// # UI Components (ui_chrome.go, ui_overlay.go, ui_overlaybox.go, ui_listpicker.go, ui_grid2d.go, ui_screen.go, ui_selectlist.go, ui_spinner.go)
//
//   - [Chrome] — title / body / status vertical layout with automatic height math.
//   - [Overlay] — scrollable modal panel with compositing helpers.
//   - [OverlayBox] — styled modal overlay with title, body, and hint footer.
//   - [ListPicker] — pre-styled filterable list with one-line construction.
//   - [SelectList] — multi-select toggle list for wizard flows.
//   - [Grid2D] — reusable 2-D cursor with move, clamp, and wrap.
//   - [Screen] / [Router] — dispatch table that replaces repeated switch statements.
//   - [RunWithSpinner] / [RunWithProgress] — inline spinner and progress bar.
//
// # Forms (ui_form.go)
//
//   - [RunForm] — run a huh form with project theme, keymap, and stdin drain.
//   - [Input] / [InputInto] — single text input prompt.
//   - [Select] — single select prompt.
//   - [HuhTheme] / [HuhKeyMap] — project theming for huh forms.
//   - [ErrFormQuiet] — returned when a form needs input but quiet mode is active.
//   - [ErrFormAborted] — re-export of huh.ErrUserAborted for callers.
//
// # Runtime (run_async.go, run_program.go, run_drain.go, run_quiet.go)
//
//   - [AsyncCmd] / [AsyncCmdCtx] — generic async tea.Cmd with [Result].
//   - [Run] / [RunModel] — launch a Bubbletea program with stdin drain.
//   - [DrainStdin] — flush stale terminal responses after tea.Program.Run().
//   - [IsQuiet] / [SetQuiet] — global toggle for non-interactive (--json) mode.
//
// All component types are designed for unit testing without teatest (plain
// structs, no tea.Model dependency) and for integration testing with teatest
// (they compose naturally inside a Bubbletea Model).
package uikit
