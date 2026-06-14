// Package uikit provides the shared visual foundation for all TUI and CLI
// output in the project. It avoids depending on the CLI framework
// (urfave/cli) and on higher-level domain packages (internal/db, internal/zot)
// so any package — including new TUI surfaces — can adopt it without
// circular-import headaches.
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
// # Layout (layout_flex.go, layout_box.go, layout_grid.go, layout_responsive.go, layout_dims.go, layout_compose.go)
//
// Flexbox-inspired layout system for terminal UIs:
//
//   - [VStack] / [HStack] — builder-style vertical/horizontal containers.
//     Children can be [Stack.Fixed] (measured, takes natural size),
//     [Stack.Flex] (proportional share of remaining space), or [Stack.Gap].
//     Conditional children via [Stack.FixedIf] / [Stack.FlexIf].
//   - [Box] — border-box rendering: set outer size + style, callback receives
//     inner dimensions after frame overhead is subtracted automatically.
//   - [GridLayout] — auto-flow N-column grid via [Grid]. Equal-width cells
//     with [GridLayout.Gap] spacing. Populate with [GridLayout.Cell] or
//     [GridLayout.Cells] (indexed callback for slices).
//   - [ResponsiveLayout] — breakpoint-driven layout switching via [Responsive].
//     Highest matching [ResponsiveLayout.When] wins; [ResponsiveLayout.Default]
//     is the fallback.
//   - [Chrome] — title/body/status chrome, now implemented as a VStack internally.
//
// Dimension constants, clamping helpers, and composition utilities (Spread,
// Center, Pad, Fit, FitHeight, WordWrap, PageLayout, SummaryLine, FooterBar,
// StatusRow) complete the layout layer.
//
// # UI Components (ui_chrome.go, ui_overlay.go, ui_overlaybox.go, ui_listpicker.go, ui_grid2d.go, ui_screen.go, ui_selectlist.go, ui_spinner.go)
//
//   - [Chrome] — title / body / status vertical layout with automatic height math.
//   - [Overlay] — scrollable modal panel with compositing helpers.
//   - [MarkdownOverlay] — scrollable modal panel that renders markdown via glamour.
//   - [ScrollableOverlay] — common interface for [Overlay] and [MarkdownOverlay].
//   - [OverlayBox] — styled modal overlay with title, body, and hint footer.
//   - [OverlayInnerWidth] / [OverlayContentWidth] / [OverlayBodyBudget] — derive an
//     overlay's inner text width and scrolling-body line budget from its frame
//     style plus measured chrome, so sizing tracks border/padding/chrome changes
//     instead of drifting from hardcoded insets.
//   - [ListCore] — shared base for every list surface: owns the bubbles list,
//     the one open/back/quit keymap, the help footer, and the filtering guard.
//     [Classify] turns a key press into an [Intent] the parent acts on, so `l`
//     means "open" everywhere (help, learn, setup, and browser.Model all embed it).
//   - [ListPicker] — flat alias of [ListCore]: a filterable list, one-line construction.
//   - [SelectList] — multi-select toggle list for wizard flows.
//   - [Grid2D] — reusable 2-D cursor with move, clamp, and wrap.
//   - [Screen] / [Router] — dispatch table that replaces repeated switch statements.
//   - [RunWithSpinner] / [RunWithProgress] — inline spinner and progress bar.
//
// # Markdown Rendering (render_md.go)
//
//   - [RenderMarkdown] / [PreRenderMarkdown] — glamour-based rendering with caching.
//   - [DetectTermStyle] — probe terminal dark/light background before TUI starts.
//   - [HighlightMatches] — ANSI-aware reverse-video search highlighting.
//
// # Forms (ui_form.go)
//
// uikit owns huh; no other package imports it. Single prompts:
//
//   - [Input] / [InputInto] — single text input prompt.
//   - [Select] / [MultiSelect] — single / multi choice prompt.
//   - [Confirm] — yes/no prompt (most callers want cmdutil.Confirm).
//
// Multi-field forms (several fields on one screen, optionally conditional):
//
//   - [NewForm] / [FormGroup] / [FormInput] / [FormSelect] — build a form;
//     run it with [Form.Run]. [Group.HideWhen] drives conditional groups.
//
// Field options ([WithDescription] / [WithPlaceholder] / [WithPassword] /
// [WithValidation]) configure both the single-prompt and form-builder inputs.
//
//   - [HuhTheme] / [HuhKeyMap] — project theming for huh forms.
//   - [ErrFormQuiet] — returned when a form needs input but quiet mode is active.
//   - [ErrFormAborted] — re-export of huh.ErrUserAborted for callers.
//
// # Text Editing (line_editor.go)
//
//   - [LineEditor] — single-line rune buffer with cursor for overlay text inputs.
//
// # Runtime (run_async.go, run_program.go, run_drain.go, run_quiet.go, run_debug.go)
//
//   - [AsyncCmd] / [AsyncCmdCtx] — generic async tea.Cmd with [Result];
//     a panic in the wrapped fn becomes a [CommandPanicMsg].
//   - [SafeCmd] — wrap a raw func() tea.Msg command so a panic becomes a
//     [CommandPanicMsg] instead of crashing the goroutine and wedging the terminal.
//   - [Run] / [RunModel] — launch a Bubbletea program with stdin drain; a
//     command panic is surfaced as [ErrCommandPanic] with the terminal restored.
//   - [DrainStdin] — flush stale terminal responses after tea.Program.Run().
//   - [IsQuiet] / [SetQuiet] — global toggle for non-interactive (--json) mode.
//   - [TUIDebugEnv] — set this env var to a file path and [Run] / [RunModel]
//     dump every tea.Msg to it (pretty-printed, tail-able) for debugging the
//     message stream. Off by default; suppressed in quiet mode; zero overhead
//     when unset.
//
// All component types are designed for unit testing without teatest (plain
// structs, no tea.Model dependency) and for integration testing with teatest
// (they compose naturally inside a Bubbletea Model).
package uikit
