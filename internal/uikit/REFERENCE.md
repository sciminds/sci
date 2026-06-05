# uikit — quick reference

Auto-generated from godoc comments. Do not edit by hand.
Regenerate with `just docs-uikit`.

Categories follow the file-prefix layout documented in [doc.go](./doc.go).
For full signatures run `go doc ./internal/uikit <Symbol>`.

## Colors

| Symbol | Kind | Description |
|---|---|---|
| `Palette` | type | Palette holds resolved colors for a specific light/dark mode. |
| `Styles` | type | Styles holds pre-built lipgloss styles shared across all TUI commands. |
| `DetectDark` | func | DetectDark returns true if the terminal has a dark background. |
| `Header` | func | Header prints a bold section header. |
| `Hint` | func | Hint prints a dimmed indented line. |
| `NewPalette` | func | NewPalette builds the Wong colorblind-safe palette for the given mode. |
| `NewStyles` | func | NewStyles creates a Styles instance for the given light/dark mode. |
| `NextStep` | func | NextStep prints a suggested next action after a command completes. |
| `OK` | func | OK prints a green check line. |
| `SymOK, SymFail, SymWarn, SymArrow` | var | Pre-rendered symbols for non-TUI CLI output. |
| `TUI` | var | TUI is the package-level shared styles singleton. |
| `IconPass, IconFail, IconWarn, IconPending, IconArrow, IconCursor, IconDot, IconSkip` | const | Icon constants — raw strings so callers can apply their own styles. |

## Input

| Symbol | Kind | Description |
|---|---|---|
| `NewHelp` | func | NewHelp creates a help.Model styled from the shared palette. |
| `BindQuit, BindUp, BindDown, BindEnter, BindHelp` | var | Shared key bindings reusable across all TUIs. |
| `KeyQ, KeyCtrlC, KeyUp, KeyDown, KeyJ, KeyK, KeySpace, KeyEnter, KeyTab, KeyShiftTab, KeyY, KeyN, KeyA, KeyEsc` | const | input_keys.go — string constants for keyboard keys, replacing bare literals in |

## Layout

| Symbol | Kind | Description |
|---|---|---|
| `GridLayout` | type | GridLayout is a builder for N-column auto-flow grid layouts. |
| `ResponsiveLayout` | type | ResponsiveLayout is a builder for breakpoint-driven layout switching. |
| `Stack` | type | Stack is a builder for composing vertical or horizontal layouts. |
| `SummaryKind` | type | SummaryKind controls how a summary part is styled. |
| `SummaryPart` | type | SummaryPart is one segment of a summary line (e.g. "3 passed"). |
| `Box` | func | Box renders content inside a styled frame, automatically computing inner |
| `Center` | func | Center centers s horizontally within width using space padding. If s is |
| `ClampHeight` | func | ClampHeight returns height if it is at least MinUsableHeight, otherwise |
| `ClampWidth` | func | ClampWidth returns ContentWidth(width) if the result is at least |
| `ContentWidth` | func | ContentWidth returns the usable inner width after subtracting |
| `Fit` | func | Fit truncates s to width cells (with ellipsis) then pads to exactly width. |
| `FitHeight` | func | FitHeight pads or truncates s so it contains exactly h newline- |
| `FitRight` | func | FitRight is [Fit] with right-alignment (numeric columns). |
| `FooterBar` | func | FooterBar renders a bottom bar with left-aligned and right-aligned content, |
| `Grid` | func | Grid creates an N-column grid layout builder. Cells flow left-to-right, |
| `HStack` | func | HStack creates a horizontal stack builder. Children are composed left-to-right. |
| `OverlayBodyHeight` | func | OverlayBodyHeight returns the maximum number of body lines available inside |
| `Pad` | func | Pad pads s to exactly width cells, aligned by pos (Left, Center, Right). |
| `PadLeft` | func | PadLeft pads s with leading spaces to exactly width cells. If s is already |
| `PadRight` | func | PadRight pads s with trailing spaces to exactly width cells. If s is already |
| `PageLayout` | func | PageLayout composes a standard TUI page: title header, body, and footer bar, |
| `Responsive` | func | Responsive creates a breakpoint-driven layout builder. Use [ResponsiveLayout.When] |
| `Spread` | func | Spread renders left-aligned and right-aligned content within a fixed width, |
| `SpreadMinGap` | func | SpreadMinGap is like [Spread] but guarantees at least minGap spaces between |
| `StatusRow` | func | StatusRow renders a standard indented icon + label line used in phase views. |
| `SummaryLine` | func | SummaryLine renders a "N label · N label · …" summary. Zero-count parts |
| `Truncate` | func | Truncate shortens s to at most width cells, appending an ellipsis (…) when |
| `VStack` | func | VStack creates a vertical stack builder. Children are composed top-to-bottom. |
| `WordWrap` | func | WordWrap wraps text at maxW, preserving paragraph breaks (newlines). |
| `DividerInset` | const | DividerInset is the total horizontal inset for RenderDivider: |
| `DividerLeadingSpaces` | const | DividerLeadingSpaces is the indent prefix prepended to every divider. |
| `FallbackDividerWidth` | const | FallbackDividerWidth is the divider width used when terminal width |
| `FallbackHeight` | const | FallbackHeight is the default list/table height assumed when the |
| `FallbackWidth` | const | FallbackWidth is the default width assumed when the real width is |
| `ItemDescIndent` | const | ItemDescIndent is the indent for the second line (description) of a |
| `MaxDividerWidth` | const | MaxDividerWidth is the maximum width for horizontal dividers in TUI views. |
| `MinUsableHeight` | const | MinUsableHeight is the minimum usable body height. Below this we |
| `MinUsableWidth` | const | MinUsableWidth is the minimum terminal width we try to render for. |
| `OverlayBoxPadding` | const | OverlayBoxPadding is the total horizontal chrome of OverlayBox: |
| `OverlayChromeLines` | const | OverlayChromeLines is the vertical overhead of the overlay frame: |
| `OverlayMargin` | const | OverlayMargin is the horizontal margin from terminal edges for overlays. |
| `OverlayMaxW` | const | OverlayMaxW is the maximum overlay width. |
| `OverlayMinH` | const | OverlayMinH is the minimum viewport body height. |
| `OverlayMinW` | const | OverlayMinW is the minimum overlay width. |
| `PageChromeLines` | const | PageChromeLines is the number of vertical lines consumed by |
| `PageSidePadding` | const | PageSidePadding is the horizontal padding applied by Page() style |
| `SummarySuccess, SummaryDanger, SummaryDim` | const | SummaryKind constants for styling summary line segments. |

## Components

| Symbol | Kind | Description |
|---|---|---|
| `Action` | type | Action is a single entry in an [ActionMenu]. |
| `ActionMenu` | type | ActionMenu is a single-select cursor menu for "pick one action" overlays. |
| `Cast` | type | Cast holds a parsed asciicast v2 recording. |
| `CastEvent` | type | CastEvent is a single output event: [time, "o", data]. |
| `CastHeader` | type | CastHeader is the first line of an asciicast v2 recording. |
| `CastPlayer` | type | CastPlayer is a bubbletea sub-model that plays back an asciicast recording. |
| `CastTickMsg` | type | CastTickMsg advances playback to the given event index. |
| `Chrome` | type | Chrome renders a three-part vertical layout: title bar, body, and |
| `Grid2D` | type | Grid2D is a 2-D cursor for grid-like layouts (table columns × rows, |
| `ListPicker` | type | ListPicker wraps [list.Model] with the standard project styling: |
| `MarkdownOverlay` | type | MarkdownOverlay is a scrollable content panel that renders markdown via |
| `MdViewer` | type | MdViewer is a scrollable, searchable markdown viewer sub-model. Embed in |
| `Overlay` | type | Overlay is a scrollable content panel rendered as a modal over other content. |
| `OverlayBox` | type | OverlayBox renders a modal-style overlay with a title section, body |
| `ProgressTracker` | type | ProgressTracker is the handle passed to the callback in RunWithProgress. |
| `Router` | type | Router maps screen IDs to Screen definitions. The zero value of a |
| `Screen` | type | Screen bundles the four per-screen callbacks that Bubbletea models |
| `SelectList` | type | SelectList is a reusable Bubble Tea model for a toggle-select list. |
| `SelectListKeys` | type | SelectListKeys is the help.KeyMap for the selecting phase. |
| `SplitView` | type | SplitView composes two ScrollPanels into a responsive layout: side-by-side |
| `Toast` | type | Toast is a single notification. It is a plain value — the ToastModel |
| `ToastLevel` | type | ToastLevel represents the severity of a toast notification. |
| `ToastModel` | type | ToastModel manages a stack of auto-dismissing toast notifications. |
| `ScrollPanel` | interface | ScrollPanel is a side-by-side-ready sub-model. Each panel owns its own |
| `ScrollableOverlay` | interface | ScrollableOverlay is the common interface satisfied by both [Overlay] |
| `SelectItem` | interface | SelectItem is the interface that items in a SelectList must implement. |
| `OverlayOption` | func type | OverlayOption configures an [Overlay] or [MarkdownOverlay] at construction. |
| `SelectListOption` | func type | SelectListOption configures a SelectList. |
| `CancelFaint` | func | CancelFaint wraps each line with SGR 22 (cancel faint) so overlay text |
| `CenterOverlay` | func | CenterOverlay composites fg centered over bg. Both are newline-delimited |
| `Compose` | func | Compose is a convenience for CenterOverlay(CancelFaint(fg), DimBackground(bg)). |
| `DimBackground` | func | DimBackground applies faint (SGR 2) to every line of s. |
| `HardenListKeyMap` | func | HardenListKeyMap frees the vim half-page keys (d/u/b/f) from the list's |
| `Items` | func | Items converts a typed slice to []list.Item so callers don't need to |
| `NewActionMenu` | func | NewActionMenu creates an action menu. The cursor starts on the first |
| `NewCastPlayer` | func | NewCastPlayer creates a player for the given cast recording. |
| `NewListDelegate` | func | NewListDelegate returns a list.DefaultDelegate styled to match the TUI theme. |
| `NewListPicker` | func | NewListPicker creates a pre-styled filterable list. The hints (if |
| `NewMarkdownOverlay` | func | NewMarkdownOverlay creates an auto-sized markdown overlay. The content is |
| `NewMdViewer` | func | NewMdViewer creates a viewer for a single markdown document. |
| `NewOverlay` | func | NewOverlay creates an auto-sized overlay. The viewport height shrinks to |
| `NewRouter` | func | NewRouter builds a Router from a set of screen registrations. |
| `NewSelectList` | func | NewSelectList creates a new SelectList with the given items. |
| `NewSelectListKeys` | func | NewSelectListKeys returns the default key map for a select list. |
| `NewSplitView` | func | NewSplitView creates a split view titled with the given string. Left is |
| `NewToastModel` | func | NewToastModel returns an empty toast manager showing up to 5 toasts. |
| `OverlayWidth` | func | OverlayWidth computes the overlay content width given terminal width and |
| `ParseCast` | func | ParseCast parses asciicast v2 format (JSON-lines: header object + event arrays). |
| `RenderSelectItemLine` | func | RenderSelectItemLine renders the cursor/marker/name skeleton common to all |
| `RunMdViewer` | func | RunMdViewer launches a full-screen markdown viewer for the file at path. |
| `RunWithProgress` | func | RunWithProgress shows an inline progress display while fn runs. The |
| `RunWithSpinner` | func | RunWithSpinner shows an inline spinner while fn runs. Returns fn's error. |
| `RunWithSpinnerStatus` | func | RunWithSpinnerStatus shows an inline spinner while fn runs, with a |
| `WithHeading` | func | WithHeading sets the heading displayed above the list. |
| `WithInitialQuery` | func | WithInitialQuery seeds the overlay's /-search with the given query so the |
| `WithRenderItem` | func | WithRenderItem sets a custom item renderer. |
| `WithSelected` | func | WithSelected sets the initial selection state for each item by index. |

## Markdown

| Symbol | Kind | Description |
|---|---|---|
| `DetectTermStyle` | func | DetectTermStyle probes the terminal for dark/light background. |
| `HighlightMatches` | func | HighlightMatches injects reverse-video ANSI escapes around case-insensitive |
| `HighlightMatchesTokens` | func | HighlightMatchesTokens is like [HighlightMatches] but takes whitespace-split |
| `PreRenderMarkdown` | func | PreRenderMarkdown renders and caches multiple markdown documents at the given |
| `RenderMarkdown` | func | RenderMarkdown converts markdown to terminal-styled output at the given width. |

## Forms

| Symbol | Kind | Description |
|---|---|---|
| `Field` | type | Field is one prompt inside a multi-field [Form]. Build it with FormInput or |
| `Form` | type | Form is a multi-field, optionally multi-screen prompt — the sanctioned |
| `Group` | type | Group is one screen of a [Form] — a set of fields shown together. Build it |
| `Option` | type | Option is a single Select/MultiSelect choice. It aliases huh.Option so |
| `InputOption` | func type | InputOption configures an Input or InputInto prompt. |
| `Confirm` | func | Confirm renders a yes/no prompt and reports the user's choice. defaultYes |
| `FormGroup` | func | FormGroup bundles fields onto a single form screen. |
| `FormInput` | func | FormInput is the multi-field-form counterpart of InputInto: a single-line |
| `FormSelect` | func | FormSelect is the multi-field-form counterpart of Select: a single-choice |
| `HuhKeyMap` | func | HuhKeyMap returns a huh.KeyMap with esc and q added to the Quit binding |
| `HuhTheme` | func | HuhTheme returns a huh.ThemeFunc built from the project's Wong |
| `Input` | func | Input prompts for a single text value. Returns ("", ErrFormAborted) if |
| `InputInto` | func | InputInto prompts for a single text value, writing the result into *dst. |
| `MultiSelect` | func | MultiSelect prompts the user to tick zero or more options from a list |
| `NewForm` | func | NewForm assembles groups into a runnable form. Each group is one screen, |
| `NewOption` | func | NewOption builds a Select/MultiSelect choice: label is shown to the user, |
| `Select` | func | Select prompts the user to pick one option from a list. Returns the zero |
| `WithDescription` | func | WithDescription sets the dimmed help line shown under a field's title. Input |
| `WithPassword` | func | WithPassword masks the input (dots instead of characters), for secrets |
| `WithPlaceholder` | func | WithPlaceholder sets greyed-out placeholder text inside the input. |
| `WithValidation` | func | WithValidation attaches a validation function to the input. |
| `ErrFormAborted` | var | ErrFormAborted is re-exported from huh so callers can check for user |
| `ErrFormQuiet` | var | ErrFormQuiet is returned when a form would need interactive input but |

## Text Editing

| Symbol | Kind | Description |
|---|---|---|
| `LineEditor` | type | LineEditor is a reusable single-line rune buffer with cursor management. |
| `NewLineEditor` | func | NewLineEditor creates a LineEditor pre-filled with text and the cursor at the end. |

## Runtime

| Symbol | Kind | Description |
|---|---|---|
| `Result` | type | Result is a generic outcome from an async command. Use in a type switch |
| `AsyncCmd` | func | AsyncCmd wraps a fallible function into a tea.Cmd that returns |
| `AsyncCmdCtx` | func | AsyncCmdCtx wraps a context-aware function with a timeout into a |
| `DrainStdin` | func | DrainStdin flushes any bytes pending in the stdin buffer. This absorbs |
| `DrainStdin` | func | DrainStdin flushes any bytes pending in the stdin buffer. This absorbs |
| `IsQuiet` | func | IsQuiet reports whether quiet mode is active. |
| `Run` | func | Run launches a Bubbletea program and returns its error. It drains |
| `RunModel` | func | RunModel launches a Bubbletea program and returns the final model |
| `SetQuiet` | func | SetQuiet enables or disables quiet (non-interactive) mode. |

