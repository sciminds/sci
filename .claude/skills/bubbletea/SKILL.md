---
name: bubbletea
description: Build terminal user interfaces with Go and Bubbletea framework. Use for creating TUI apps with the Elm architecture, dual-pane layouts, accordion modes, mouse/keyboard handling, Lipgloss styling, and reusable components. Includes production-ready templates, effects library, and battle-tested layout patterns from real projects.
license: MIT
---

# Bubbletea TUI Development

Skill for building beautiful terminal user interfaces with Go, Bubble Tea, and Lip Gloss. Pairs with the project's `lipgloss` skill (styling fundamentals) and `lo` skill (slice/map transforms).

## When to Use This Skill

- Creating new TUI applications or screens
- Adding Bubble Tea components to existing apps
- Fixing layout/rendering issues (borders, alignment, overflow)
- Implementing mouse/keyboard interactions
- Building dual-pane or multi-panel layouts
- Adding visual effects (metaballs, waves, rainbow, layered compositing)
- Troubleshooting TUI rendering problems
- Writing integration tests for Bubble Tea models with `teatest`

## Project conventions (read first)

This repo (`sci-go`) mandates **Bubble Tea v2 + Bubbles v2 + Lip Gloss v2**. No v1 imports — see `CLAUDE.md`. The major v2 deltas you'll hit:

| v1 | v2 |
|---|---|
| `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| `tea.KeyMsg` | `tea.KeyPressMsg` |
| `tea.MouseMsg` (struct, `.Type`, `.X`, `.Y`) | `tea.MouseMsg` is an interface; switch on `tea.MouseClickMsg`, `tea.MouseReleaseMsg`, `tea.MouseWheelMsg`, `tea.MouseMotionMsg`; coords via `.Mouse()` |
| `tea.WithAltScreen()`, `tea.WithMouseCellMotion()` (program options) | Declared on the returned `View()` value (declarative) |
| `lipgloss.Color("#hex")` returns `lipgloss.Color` (string-ish) | Returns `image/color.Color`; types accept any `image/color.Color` |
| `progress.WithGradient(...)` | `progress.WithColors(lipgloss.Color(...), lipgloss.Color(...))` |
| `bubbles.NewModel(...)` aliases | Removed — call `New(...)` directly |

Other project rules that affect TUI work:

- **`huh` forms must go through `uikit`.** Use `uikit.RunForm` / `uikit.Input` / `uikit.InputInto` / `uikit.Select`. Confirmations: `cmdutil.Confirm`/`ConfirmYes`. Never `.Run()` a `huh` form directly.
- **No inline `lipgloss.NewStyle()`** outside `internal/uikit/` or `internal/tui/*/ui/`. Use the `uikit.TUI` singleton.
- **New TUI apps** live under `internal/tui/<name>/` with the `dbtui` split (`app/`, `ui/`, root-pkg `Run` entry).
- **Tests:** `teatest` for every Bubble Tea model — full `key → Update → View` loop. Protocol in `internal/tui/dbtui/app/TESTING.md`.
- **`uikit` first.** Catalog in `internal/uikit/doc.go`. Extend uikit when a pattern appears in ≥ 2 TUIs.

## The 4 Golden Rules (summary)

**CRITICAL:** Before implementing ANY layout, consult `references/golden-rules.md`. These rules prevent the most common and frustrating TUI layout bugs.

1. **Always account for borders** — subtract 2 from height calculations BEFORE rendering panels
2. **Never auto-wrap in bordered panels** — always truncate text explicitly
3. **Match mouse detection to layout** — X coords for horizontal, Y coords for vertical
4. **Use weights, not pixels** — proportional layouts scale across terminal sizes

Full details and examples in `references/golden-rules.md`.

## Layout implementation pattern

The standard v2 sequence for a screen with title, status, and bordered content:

### 1. Calculate available space (account for borders!)

```go
func (m model) calculateLayout() (int, int) {
    contentWidth := m.width
    contentHeight := m.height

    if m.config.UI.ShowTitle {
        contentHeight -= 3 // title bar (3 lines)
    }
    if m.config.UI.ShowStatus {
        contentHeight -= 1 // status bar
    }
    contentHeight -= 2 // CRITICAL: top + bottom borders

    return contentWidth, contentHeight
}
```

### 2. Use weight-based panel sizing

```go
leftWeight, rightWeight := 1, 1
if m.accordionMode && m.focusedPanel == "left" {
    leftWeight = 2 // focused panel gets 2x weight
}

totalWeight := leftWeight + rightWeight
leftWidth := (availableWidth * leftWeight) / totalWeight
rightWidth := availableWidth - leftWidth
```

### 3. Truncate text to prevent wrapping

```go
maxTextWidth := panelWidth - 4 // -2 borders, -2 padding

title    = truncateString(title, maxTextWidth)
subtitle = truncateString(subtitle, maxTextWidth)

func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-1] + "…"
}
```

> For multi-byte text, prefer `runewidth.Truncate` from `github.com/mattn/go-runewidth`. The byte-slice version above is safe for ASCII only.

## Keyboard pattern (v2)

```go
import tea "charm.land/bubbletea/v2"

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
        case "tab":
            m.focusNext()
        }
    }
    return m, nil
}
```

For binding-driven matching (preferred for anything beyond a handful of keys):

```go
import "charm.land/bubbles/v2/key"

var keys = struct {
    Quit, Tab key.Binding
}{
    Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
    Tab:  key.NewBinding(key.WithKeys("tab"),         key.WithHelp("tab", "switch panel")),
}

case tea.KeyPressMsg:
    switch {
    case key.Matches(msg, keys.Quit):
        return m, tea.Quit
    case key.Matches(msg, keys.Tab):
        m.focusNext()
    }
```

## Mouse pattern (v2)

`tea.MouseMsg` is an interface in v2. Always check layout mode before processing mouse coordinates.

```go
case tea.MouseClickMsg:
    pos := msg.Mouse()
    return m.handleClick(pos.X, pos.Y), nil
case tea.MouseWheelMsg:
    pos := msg.Mouse()
    return m.handleWheel(msg.Delta(), pos.X, pos.Y), nil
}

func (m model) handleClick(x, y int) tea.Model {
    if m.shouldUseVerticalStack() {
        topHeight, _ := m.calculateVerticalStackLayout()
        relY := y - contentStartY
        if relY < topHeight {
            m.focusedPanel = "left"  // top
        } else {
            m.focusedPanel = "right" // bottom
        }
    } else {
        leftWidth, _ := m.calculateDualPaneLayout()
        if x < leftWidth {
            m.focusedPanel = "left"
        } else {
            m.focusedPanel = "right"
        }
    }
    return m
}
```

## Common pitfalls

### ❌ Don't set explicit `Height()` on bordered panels

```go
// BAD: causes off-by-one alignment bugs
panelStyle := lipgloss.NewStyle().Border(border).Height(height)
```

### ✅ Fill content to exact height; let the border add naturally

```go
for len(lines) < innerHeight {
    lines = append(lines, "")
}
panelStyle := lipgloss.NewStyle().Border(border) // no Height()
```

See `references/troubleshooting.md` for the full debugging decision tree.

## Reference files

Loaded progressively as needed:

- **`references/golden-rules.md`** — Critical layout patterns and anti-patterns. **Read before any new layout.**
- **`references/components.md`** — Catalog of reusable components (panels, lists, dialogs, menus, tables, previews) and how they map to `bubbles` and `huh`.
- **`references/effects.md`** — Animation primitives (`tea.Tick`/`tea.Every`), color cycling, Harmonica springs, layered compositing with `lipgloss.NewLayer`/`NewCompositor`, plus recipes for rainbow text, sine waves, metaballs, typewriter, and matrix rain. **Read for any animated UI.**
- **`references/troubleshooting.md`** — Common issues and decision tree for layout, mouse, rendering, keyboard, performance, and config bugs.
- **`references/emoji-width-fix.md`** — Battle-tested fix for emoji alignment across xterm, WezTerm, Termux, Windows Terminal. Apply when icons in a list/tree drift by 1 column.
- **`references/teatest.md`** — Full reference for `github.com/charmbracelet/x/exp/teatest` — API, six patterns, eleven gotchas, v1 vs v2 differences.

## Integration testing with teatest

Full message-loop tests (key → `Update` → state → `View`) use `github.com/charmbracelet/x/exp/teatest` (or `exp/teatest/v2` for the v2 message types). It's experimental but Charm uses it internally — reliable, poorly documented.

Core pattern:

```go
tm := teatest.NewTestModel(t, initialModel(), teatest.WithInitialTermSize(80, 24))
t.Cleanup(func() { _ = tm.Quit() })

tm.Type("abc")
tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter}) // v2

teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
    return bytes.Contains(out, []byte("done"))
}, teatest.WithDuration(2*time.Second))

fm := tm.FinalModel(t, teatest.WithFinalTimeout(time.Second)).(*myModel)
```

Key things to know (full details in `references/teatest.md`):

- **Always** pass `WithInitialTermSize` in v1 — no default, and models using viewport/list render broken without a `WindowSizeMsg`.
- **Never** `time.Sleep` — use `WaitFor` to block on an output condition, then fire the next input.
- **`tm.Output()` drains** — every `WaitFor` consumes bytes, so `FinalOutput` afterward only sees the tail. Do all polling via `WaitFor`, or buffer reads yourself.
- **v1 `Type()` iterates bytes** — non-ASCII breaks. Use `tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ñ'}})` (v1) or the corresponding v2 `KeyPressMsg`. v2 (`exp/teatest/v2`) fixes the byte-iteration bug.
- **Golden files** live at `testdata/<tb.Name()>.golden` and bake in terminal size + color profile. Pin `lipgloss.SetColorProfile(termenv.Ascii)` and add `*.golden -text` to `.gitattributes`.
- **Cast `FinalModel` to your concrete type** to assert on internal state — usually more robust than output-based assertions. Pin a pseudo-version ≥ `b6045cb4` (2025-10-02) for nil-safety.
- Update goldens with `go test ./path -run TestName -update`.

## Animations and effects

Bubble Tea has no built-in frame loop — animation is a `tea.Tick` that re-arms itself on each tick. The full catalog lives in `references/effects.md`:

- Frame-loop fundamentals (`tea.Tick` vs `tea.Every`, stopping cleanly, dirty-frame skipping)
- Color cycling (HSL rainbows, two-color pulses, Lip Gloss `BorderForegroundBlend`)
- Spring physics with [`harmonica`](https://github.com/charmbracelet/harmonica) for smooth slide-ins and settling values
- Layered compositing with `lipgloss.NewLayer` + `lipgloss.NewCompositor` (v2 only)
- Recipes: scrolling sine wave, metaballs/lava-lamp, typewriter reveal, matrix rain, custom spinner
- Performance: profiling, style caching, adaptive FPS
- Testing animated frames with synthetic `frameMsg`s

## Dependencies (v2)

**Required:**
```
charm.land/bubbletea/v2
charm.land/bubbles/v2
charm.land/lipgloss/v2
```

**Common additions:**
```
github.com/charmbracelet/harmonica          // spring physics
github.com/charmbracelet/x/exp/teatest      // integration tests (or .../v2)
github.com/mattn/go-runewidth               // unicode-safe width/truncation
```

**Optional, common in this repo:**
```
github.com/charmbracelet/glamour            // markdown rendering
github.com/charmbracelet/huh                // forms (must go through uikit.RunForm)
github.com/alecthomas/chroma/v2             // syntax highlighting
github.com/evertras/bubble-table            // interactive tables
```

## External resources

- [Bubble Tea v2 docs](https://pkg.go.dev/charm.land/bubbletea/v2)
- [Lip Gloss v2 docs](https://pkg.go.dev/charm.land/lipgloss/v2)
- [Bubbles v2 components](https://github.com/charmbracelet/bubbles/tree/v2.0.0)
- [Bubble Tea v2 upgrade guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md)
- [Bubbles v2 upgrade guide](https://github.com/charmbracelet/bubbles/blob/v2.0.0/UPGRADE_GUIDE_V2.md)
- [Lip Gloss v2 upgrade guide](https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md)
- [Charm ecosystem](https://charm.sh/)

## Best practices summary

1. **Read `golden-rules.md` before any new layout** — saves hours of debugging.
2. **Weight-based sizing** — never hardcode pixel widths.
3. **Truncate text explicitly** — never rely on auto-wrap inside bordered panels.
4. **Match mouse detection to layout orientation** — X for horizontal, Y for vertical.
5. **Account for borders** — subtract 2 from height before rendering.
6. **Never set explicit `Height()` on bordered Lip Gloss styles.**
7. **Forms through `uikit.RunForm`**, never `huh.Form.Run()` directly.
8. **Tests through `teatest`** — every `Update` path covered.
9. **For animation, drive frames via `tea.Tick` + custom `frameMsg`** — re-arm in `Update`.

Follow these and you'll avoid 90% of TUI layout bugs.
