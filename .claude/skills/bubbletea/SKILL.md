---
name: bubbletea
description: Build terminal user interfaces with Go and Bubbletea framework. Use for creating TUI apps with the Elm architecture, dual-pane layouts, accordion modes, mouse/keyboard handling, Lipgloss styling, and reusable components. Includes an effects/animation reference, teatest integration-testing patterns, and battle-tested layout rules from real projects.
license: MIT
---

# Bubbletea TUI Development

Skill for building beautiful terminal user interfaces with Go, Bubble Tea, and Lip Gloss. This skill owns the **Bubble Tea layer** — the Elm/MVU loop, layout, mouse/keyboard, effects, and testing. It delegates the neighbouring layers to dedicated skills so nothing is duplicated (or drifts out of sync):

- **`lipgloss` skill** — styling mechanics: `Width` vs `MaxWidth`, frame-size accounting, borders, `Join`/`Place`, color, tables/trees.
- **`huh` skill** — form/prompt internals (field types, dynamic forms, validation, theming). In *this repo* forms run through `uikit`, never `huh` directly.
- **`lo` skill** — slice/map/set transforms (the project standard over hand-rolled loops).

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

- **`uikit` first — it encodes most of the patterns below.** `internal/uikit/` is the shared visual foundation: a flexbox layout system (`VStack`/`HStack`/`Box`/`GridLayout`/`Responsive`/`Chrome`), overlays, list/select pickers, markdown rendering, spinners, and the `Run`/`RunModel` launchers. Skim `internal/uikit/doc.go` before hand-rolling anything; extend uikit when a pattern appears in ≥ 2 TUIs.
- **Styling goes through `uikit.TUI`, never inline `lipgloss.NewStyle()`.** Inline `NewStyle()` is allowed *only* inside `internal/uikit/*.go` (enforced by `rules/no-inline-newstyle.yml`). Everywhere else: semantic styles via `uikit.TUI` accessors (`.Title()`, `.Error()`, …) and raw containers via `uikit.TUI.Base()`. There is **no** per-TUI `ui/` package — styles never live next to the model. For lipgloss mechanics (Width vs MaxWidth, frame-size math, `Join`/`Place`, borders), use the **`lipgloss` skill**.
- **`uikit` owns `huh` — no other package may import it** (lint-guard rule 15; there is no `uikit.RunForm`). Single prompts: `uikit.Input` / `uikit.InputInto` / `uikit.Select` / `uikit.MultiSelect` (options via `uikit.NewOption`, configured with `uikit.WithValidation` / `uikit.WithPassword` / `uikit.WithDescription`). Multi-field forms: `uikit.NewForm(uikit.FormGroup(uikit.FormInput(…), uikit.FormSelect(…))).Run()`, with `FormGroup(...).HideWhen(cond)` for conditional pages. Confirmations: `cmdutil.Confirm` / `ConfirmYes` / `ConfirmRequired` / `ConfirmOrSkip`. For `huh` internals (field types, dynamic forms, theming), use the **`huh` skill**.
- **New TUI apps** live under `internal/tui/<name>/` with an `app/` subpackage (`model.go`, `update.go`, `view.go`, `keys.go`, `run.go`) and a thin root-pkg entry point that calls `uikit.Run` / `uikit.RunModel`. Mirror `internal/tui/dbtui/`; read `internal/tui/BUBBLE_TEA_PRIMER.md` first if you're new to the MVU pattern.
- **Clickable elements are zone-marked, not coordinate-matched.** The project uses `github.com/lrstanley/bubblezone/v2`. Mark regions with `zone.Mark(id, content)`, scan the final frame with `m.zones.Scan(view)`, and hit-test with `m.zones.Get(id).InBounds(msg)` — see the Mouse pattern below. `dbtui/CLAUDE.md` requires this for every clickable element.
- **Slice/map/set transforms** use `samber/lo` (project rule, audience is Python/JS devs) — see the **`lo` skill**.
- **Tests:** `teatest` for every Bubble Tea model — full `key → Update → View` loop. Protocol in `internal/tui/dbtui/app/TESTING.md`.

## The 4 Golden Rules (and how uikit encodes them)

These four rules prevent the most common TUI layout bugs. In this project you mostly get them **for free** by reaching for the uikit layout primitives — but you still need to understand *why*, because the moment you drop to raw lipgloss the rules become yours to enforce again. Read `references/golden-rules.md` before any new hand-rolled layout.

| Rule | Why it matters | The uikit primitive that handles it |
|---|---|---|
| **1. Account for borders** | Borders add 2 rows/cols; forget them and panels overflow the title/status bar | `uikit.Box(w, h, style, fn)` — the callback receives *inner* dims with frame overhead already subtracted |
| **2. Never auto-wrap in bordered panels** | A single wrapped line silently makes a panel taller and misaligns its neighbour | `uikit.Truncate` / `uikit.Fit` / `uikit.WordWrap` — explicit width control instead of lipgloss auto-wrap |
| **3. Hit-test mouse against the layout** | A click maps to the wrong panel if detection ignores how the layout reflowed | `bubblezone` — `zone.Mark` + `zones.Get(id).InBounds(msg)` removes coordinate math entirely |
| **4. Use weights, not pixels** | Hardcoded widths break on resize and on portrait terminals | `uikit.VStack/HStack(...).Flex(ratio, fn)` — proportional shares of the remaining space |

When a layout can be expressed with `VStack`/`HStack`/`Box`/`GridLayout`/`Responsive`, use them — those primitives *are* the rules made executable. Hand-roll the math only when you genuinely can't, then consult the reference.

## Layout implementation pattern

**Reach for the uikit primitives first.** A title/body/status screen with a weighted dual pane is a few lines — and it gets the border math and proportional sizing right by construction:

```go
import "github.com/sciminds/cli/internal/uikit"

func (m model) render() string {
    return uikit.VStack(m.width, m.height).
        Fixed(func(w int) string { return m.renderTitle(w) }).   // natural height
        Flex(1, func(w, h int) string {                          // fills remaining height
            return uikit.HStack(w, h).
                Flex(m.leftRatio, func(w, h int) string { return m.renderLeft(w, h) }).
                Flex(m.rightRatio, func(w, h int) string { return m.renderRight(w, h) }).
                Render()
        }).
        Fixed(func(w int) string { return m.renderStatus(w) }).
        Render()
}
```

`Flex(ratio, …)` *is* Golden Rule #4 (weights, not pixels): give the focused pane a larger ratio for accordion mode (`m.leftRatio = 2`) and the split re-proportions automatically on resize. Each callback receives the exact inner dimensions, so children never need to know the parent's size. Real example: `internal/tui/dbtui/app/view.go`.

For a single bordered panel, `uikit.Box` does the border accounting (Golden Rule #1) for you:

```go
panel := uikit.Box(w, h, uikit.TUI.Base().Border(lipgloss.RoundedBorder()),
    func(innerW, innerH int) string {
        // innerW/innerH already exclude the border + padding
        return uikit.Truncate(m.title, innerW) // Golden Rule #2: explicit, no auto-wrap
    })
```

### When you hand-roll the math

If you're dropping to raw lipgloss for a layout uikit doesn't cover, the rules become yours to enforce. The manual sequence is: subtract title/status/border rows from the height *before* rendering, derive widths from weights (`(avail * weight) / totalWeight`), and truncate **every** string. Full worked examples in `references/golden-rules.md`.

Use `uikit.Truncate(s, width)` — it is rune- and ANSI-aware. **Never** `s[:n]`: byte-slicing corrupts multi-byte text and miscounts emoji/CJK width (see `references/emoji-width-fix.md`). `mattn/go-runewidth`'s `runewidth.Truncate` is the equivalent if you're outside the repo.

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

`tea.MouseMsg` is an interface in v2 — switch on `tea.MouseClickMsg` / `tea.MouseReleaseMsg` / `tea.MouseWheelMsg` / `tea.MouseMotionMsg`. This project hit-tests with **`github.com/lrstanley/bubblezone/v2`**, not coordinate math (Golden Rule #3): you mark clickable regions when rendering and ask the zone manager which one a click landed in — so the answer stays correct no matter how the layout reflowed.

**1. Hold a manager on the model; scan the final frame in `View`:**

```go
import zone "github.com/lrstanley/bubblezone/v2"

type model struct {
    zones *zone.Manager
    // …
}
func newModel() model { return model{zones: zone.New()} }

func (m model) View() tea.View {
    v := tea.NewView(m.zones.Scan(m.render())) // Scan rewrites the marks into real coords
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

**2. Mark each clickable region while rendering it:**

```go
left  := m.zones.Mark("panel-left", m.renderLeft(w, h))
right := m.zones.Mark("panel-right", m.renderRight(w, h))
```

**3. Hit-test in the handler — no X/Y arithmetic:**

```go
case tea.MouseClickMsg:
    switch {
    case m.zones.Get("panel-left").InBounds(msg):
        m.focusedPanel = "left"
    case m.zones.Get("panel-right").InBounds(msg):
        m.focusedPanel = "right"
    }
    return m, nil
case tea.MouseWheelMsg:
    m.scroll += msg.Delta() * 3 // wheel is its own message; Delta() is signed
```

The manual coordinate approach (compare `msg.Mouse().X` to panel widths, branching on orientation) still works and is kept in `references/golden-rules.md` Rule #3 as the fallback — but zone-marking is the project convention, and it's the reason you rarely need that math.

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
- **`references/components.md`** — "What to reach for" catalog: the `uikit` → `bubbles/v2` → hand-roll decision order for layout, lists, inputs, overlays, tables, and status bars, plus message-routing patterns for composing them.
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
github.com/lrstanley/bubblezone/v2          // mouse zone hit-testing (project standard)
github.com/charmbracelet/harmonica          // spring physics
github.com/charmbracelet/x/exp/teatest      // integration tests (or .../v2)
github.com/mattn/go-runewidth               // unicode-safe width/truncation (prefer uikit.Truncate in-repo)
```

**Used via `uikit`, not imported directly:**
```
github.com/charmbracelet/glamour            // markdown — via uikit.RenderMarkdown / MarkdownOverlay
charm.land/huh/v2                           // forms — via uikit.Input/Select/NewForm (rule 15 bans direct import)
```

**Not currently imported — add deliberately if a feature needs it:**
```
github.com/alecthomas/chroma/v2             // syntax highlighting (no current dep)
```
> For tables, use the built-in `charm.land/bubbles/v2/table` (what `dbtui` uses), not `evertras/bubble-table`. For filtering, use the `bubbles/v2/list` fuzzy filter before pulling in a separate finder.

## External resources

- [Bubble Tea v2 docs](https://pkg.go.dev/charm.land/bubbletea/v2)
- [Lip Gloss v2 docs](https://pkg.go.dev/charm.land/lipgloss/v2)
- [Bubbles v2 components](https://github.com/charmbracelet/bubbles/tree/v2.0.0)
- [Bubble Tea v2 upgrade guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md)
- [Bubbles v2 upgrade guide](https://github.com/charmbracelet/bubbles/blob/v2.0.0/UPGRADE_GUIDE_V2.md)
- [Lip Gloss v2 upgrade guide](https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md)
- [Charm ecosystem](https://charm.sh/)

## Best practices summary

1. **`uikit` first** — `VStack`/`HStack`/`Box`/`GridLayout` encode the golden rules; reach for them before hand-rolling layout math.
2. **Read `golden-rules.md` before any *hand-rolled* layout** — saves hours of debugging.
3. **Weight-based sizing** (`.Flex(ratio, …)`) — never hardcode pixel widths.
4. **Truncate explicitly with `uikit.Truncate`** — never rely on auto-wrap inside bordered panels, never byte-slice (`s[:n]`).
5. **Mouse via `bubblezone`** — `zone.Mark` + `zones.Get(id).InBounds(msg)`, not coordinate math.
6. **Account for borders** — or let `uikit.Box` do it; never set explicit `Height()` on a bordered lipgloss style.
7. **Forms via `uikit`** — `uikit.Input`/`Select`/`NewForm`, confirmations via `cmdutil.Confirm`. There is no `uikit.RunForm`, and direct `huh` imports are banned (rule 15).
8. **Styles via `uikit.TUI`** — no inline `lipgloss.NewStyle()` outside `internal/uikit/`, no per-TUI `ui/` package.
9. **Tests through `teatest`** — every `Update` path covered.
10. **For animation, drive frames via `tea.Tick` + custom `frameMsg`** — re-arm in `Update`.

Follow these and you'll avoid 90% of TUI layout bugs.
