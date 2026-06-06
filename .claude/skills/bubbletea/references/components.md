# Bubble Tea Components Catalog

What to reach for when you need a list, a panel, an input, an overlay, or a status bar — and in what order.

## The decision order

Before building any component, ask in this sequence:

1. **Does `uikit` already have it?** `internal/uikit/` is the shared foundation. Using it means correct styling, theming, border math, and testability for free — and it's the project rule (extend uikit when a pattern shows up in ≥ 2 TUIs). Skim `internal/uikit/doc.go`.
2. **Does `bubbles/v2` have it?** The Charm component library — `list`, `table`, `textinput`, `textarea`, `viewport`, `progress`, `spinner`, `help`, `key`. Wrap it in uikit styling, don't restyle inline.
3. **Only then hand-roll**, and put it in uikit if anyone else will want it.

> **Imports are v2, always.** `charm.land/bubbletea/v2`, `charm.land/bubbles/v2/...`, `charm.land/lipgloss/v2`. The v1 GitHub paths (`github.com/charmbracelet/...`) are banned by `CLAUDE.md`. For lipgloss styling mechanics see the **`lipgloss` skill**; for form/prompt internals see the **`huh` skill** (in this repo, forms go through `uikit`, never `huh` directly).

## Quick map: need → reach for

| You need… | First choice (`uikit`) | Underlying / fallback |
|---|---|---|
| Title/body/status chrome | `uikit.Chrome` / `uikit.VStack` | hand-rolled `VStack` |
| Weighted multi-pane layout | `uikit.HStack(...).Flex(r, fn)` | manual weight math (golden-rules #4) |
| Single bordered panel | `uikit.Box(w, h, style, fn)` | `lipgloss` border + manual inner-size math |
| Responsive (stack ↔ side-by-side) | `uikit.Responsive(w,h).When(...)` | branch on `m.width` yourself |
| N-column grid | `uikit.Grid(w, h, cols)` | manual column math |
| Filterable list / picker | `uikit.ListPicker` | `bubbles/v2/list` |
| Multi-select toggle list | `uikit.SelectList` | hand-rolled |
| Data table | `bubbles/v2/table` | — |
| Single text prompt | `uikit.Input` / `uikit.InputInto` | — (never raw `huh`/`textinput` for prompts) |
| Choose one / many | `uikit.Select` / `uikit.MultiSelect` | — |
| Multi-field form | `uikit.NewForm(uikit.FormGroup(...))` | — |
| Yes/no confirm | `cmdutil.Confirm` / `ConfirmYes` | `uikit.Confirm` |
| Inline editable text (in a model) | `uikit.LineEditor` | `bubbles/v2/textinput` |
| Modal / popover | `uikit.OverlayBox` / `uikit.Overlay` | `lipgloss` compositor (see `effects.md` §5) |
| Markdown panel | `uikit.MarkdownOverlay` / `uikit.RenderMarkdown` | `glamour` via uikit |
| Scrollable content region | `bubbles/v2/viewport` | — |
| Spinner / progress for a blocking op | `uikit.RunWithSpinner` / `RunWithProgress` | `bubbles/v2/spinner` / `progress` |
| Key help footer | `bubbles/v2/help` + `bubbles/v2/key` | `uikit` help styles |
| Mouse hit-testing | `bubblezone/v2` (`zone.Mark`/`InBounds`) | manual coords (golden-rules #3) |

All components follow the Elm architecture (`Init`/`Update`/`View`) and compose inside a parent model.

---

## Layout & chrome (uikit)

The layout primitives are the heart of uikit — they encode the golden rules (see `golden-rules.md`). Each child callback receives its exact inner dimensions, so children never need to know the parent's size.

```go
import "github.com/sciminds/cli/internal/uikit"

// Vertical stack: fixed title + flexible body + fixed status.
uikit.VStack(m.width, m.height).
    Fixed(func(w int) string { return m.renderTitle(w) }).
    Flex(1, func(w, h int) string { return m.renderBody(w, h) }).
    Fixed(func(w int) string { return m.renderStatus(w) }).
    Render()

// Horizontal split with accordion weighting (focused pane is wider).
uikit.HStack(w, h).
    Flex(m.leftRatio, func(w, h int) string { return m.renderLeft(w, h) }).
    Flex(1, func(w, h int) string { return m.renderRight(w, h) }).
    Render()

// Single bordered box — inner dims already exclude the frame.
uikit.Box(w, h, uikit.TUI.Base().Border(lipgloss.RoundedBorder()),
    func(innerW, innerH int) string { return uikit.Truncate(text, innerW) })

// Responsive: pick a layout by breakpoint. When(minWidth, fn) matches when
// width >= minWidth (highest matching wins); Default is the narrow fallback.
uikit.Responsive(m.width, m.height).
    When(80, func(w, h int) string { return sideBySide(w, h) }).
    Default(func(w, h int) string { return stacked(w, h) }).
    Render()
```

`FixedIf` / `FlexIf` add a child only when a condition holds (e.g. hide a sidebar on narrow terminals). `Gap(n)` inserts spacing. Real example: `internal/tui/dbtui/app/view.go`.

---

## Lists & selection

### `uikit.ListPicker` — filterable list, one-line construction

Pre-styled wrapper over `bubbles/v2/list`. Pass `list.Item`s and optional extra key bindings.

```go
import (
    "charm.land/bubbles/v2/list"
    "github.com/sciminds/cli/internal/uikit"
)

picker := uikit.NewListPicker("Pick a file", items) // items []list.Item
```

Use the raw `bubbles/v2/list` directly only when you need behaviour ListPicker doesn't expose. Its delegate handles fuzzy filtering (`/`), navigation (arrows, `Home`/`End`, `PgUp`/`PgDn`), and selection out of the box.

### `uikit.SelectList` — multi-select toggle list

For wizard-style "tick the ones you want" flows. Built from `uikit.SelectItem`s with `uikit.NewSelectList(items, opts...)`.

### `bubbles/v2/table` — data tables

The project standard for tabular data (not `evertras/bubble-table`). `dbtui` builds its grid on it.

```go
import "charm.land/bubbles/v2/table"

t := table.New(
    table.WithColumns([]table.Column{{Title: "ID", Width: 6}, {Title: "Name", Width: 24}}),
    table.WithRows(rows),
    table.WithFocused(true),
)
// Style via uikit.TUI; route key/mouse msgs through t.Update in your model.
```

---

## Text input, prompts & forms

Two distinct tiers — don't mix them up:

**Tier 1 — interactive prompts (blocking, outside a running model).** Use the uikit wrappers; they own `huh` and apply project theming. Never call `huh` or build a `textinput` prompt yourself.

```go
name, err := uikit.Input("Project name", "lowercase, no spaces",
    uikit.WithValidation(validateName))
err := uikit.InputInto(&cfg.Token, "API token", "", uikit.WithPassword())

choice, err := uikit.Select("Backend", []uikit.Option[string]{
    uikit.NewOption("SQLite", "sqlite"),
    uikit.NewOption("DuckDB", "duck"),
})
picks, err := uikit.MultiSelect("Features", "space to toggle", opts)

// Multi-field form, optionally conditional pages:
err := uikit.NewForm(
    uikit.FormGroup(
        uikit.FormInput(&name, "Name", uikit.WithValidation(notEmpty)),
        uikit.FormSelect(&backend, "Backend", backendOpts),
    ),
    uikit.FormGroup(
        uikit.FormInput(&dsn, "Connection string"),
    ).HideWhen(func() bool { return backend == "sqlite" }),
).Run()

// Confirmations:
if err := cmdutil.ConfirmYes("Overwrite existing file?"); err != nil { return err }
```

Field options (`uikit.WithDescription`, `WithPlaceholder`, `WithPassword`, `WithValidation`) work for both single prompts and form fields. For everything about *how* huh forms behave (field types, dynamic `Func` forms, accessibility, theming) → **`huh` skill**.

**Tier 2 — input embedded *inside* a live model.** Use the raw `bubbles` components and drive them from your `Update`:

```go
import "charm.land/bubbles/v2/textinput"

ti := textinput.New()
ti.Placeholder = "search…"
ti.Focus()
// in Update: m.ti, cmd = m.ti.Update(msg)
```

For a lightweight single-line editor inside an overlay (e.g. cell editing), `uikit.LineEditor` is a plain rune-buffer-with-cursor that needs no `tea.Model` wiring. `bubbles/v2/textarea` covers multi-line.

---

## Overlays & modals

`uikit` composites overlays onto the base view for you — you don't hand-roll the lipgloss layering.

```go
// Styled modal with title, body, and hint footer (a struct, sized at Render):
box := uikit.OverlayBox{
    Title: "Player",
    Body:  body,
    Hints: []string{"space pause/play", "esc close"},
}.Render(m.width, m.height)

// Scrollable content overlay (handles its own up/down):
ov := uikit.NewOverlay("Help", content, m.width, m.height)

// Markdown overlay (renders via glamour, scrollable):
md := uikit.NewMarkdownOverlay("README", markdown, m.width, m.height)
```

`Overlay` and `MarkdownOverlay` both satisfy `uikit.ScrollableOverlay`, so you can hold either behind one interface field. For bespoke "thing on top of thing" effects (sprites, popovers, transparency), drop to the lipgloss compositor — see `effects.md` §5.

---

## Markdown, preview & status

- **Markdown:** `uikit.RenderMarkdown(md, width)` (cached glamour render); `uikit.PreRenderMarkdown` to warm the cache; `uikit.RunMdViewer(path)` for a standalone pager. Don't import `glamour` directly.
- **Syntax-highlighted code preview:** not currently a project dependency — if you need it, add `chroma/v2` deliberately rather than assuming it's present.
- **Status / help:** compose with `uikit` chrome helpers (`StatusRow`, `FooterBar`, `SummaryLine`) and `bubbles/v2/help` + `bubbles/v2/key` for an auto-generated key legend. The status-bar spacing pattern:

```go
import "charm.land/lipgloss/v2"

func (m model) renderStatusBar() string {
    left  := fmt.Sprintf("%s | %s", m.mode, m.filename)
    right := fmt.Sprintf("Line %d/%d", m.cursor, m.lineCount)
    gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) // lipgloss.Width, not len()
    return left + strings.Repeat(" ", max(gap, 0)) + right
}
```

`lipgloss.Width` measures display cells (ANSI- and width-aware); `len()` counts bytes and will mis-space anything with color or wide runes.

---

## Progress & spinners

For a blocking operation with a live indicator, the uikit runners wrap the whole thing:

```go
err := uikit.RunWithSpinner("Fetching items…", func() error {
    return doSlowWork()
})

err := uikit.RunWithProgress("Downloading", func(t *uikit.ProgressTracker) error {
    t.SetTotal(len(items))
    for _, item := range items {
        download(item)
        t.Advance("downloaded", item.Name) // (counter bucket, status-line text)
    }
    return nil
})
```

Inside a long-lived model, embed `bubbles/v2/spinner` or `bubbles/v2/progress` and tick them yourself (`progress` uses `harmonica` springs internally — see `effects.md` §4).

---

## Composing components

The core skill of a multi-component TUI is **routing** messages to whichever child has focus, and letting children talk back via commands.

### Route to the focused component

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch m.focused {
        case focusList:
            var cmd tea.Cmd
            m.list, cmd = m.list.Update(msg)
            return m, cmd
        case focusInput:
            var cmd tea.Cmd
            m.input, cmd = m.input.Update(msg)
            return m, cmd
        }
    }
    return m, nil
}
```

### Lazy-initialise expensive children

```go
type model struct {
    preview *PreviewComponent // nil until first needed
}
func (m *model) showPreview(path string) tea.Cmd {
    if m.preview == nil {
        m.preview = NewPreviewComponent()
    }
    return m.preview.Load(path) // return the Cmd, don't block
}
```

### Let children communicate via custom messages

A child shouldn't reach into a sibling. It emits a `tea.Cmd` returning a typed message; the parent routes the result. This keeps each component testable in isolation.

```go
type fileSelectedMsg struct{ path string }

// In the list's Update, on Enter:
return m, func() tea.Msg { return fileSelectedMsg{path: m.list.SelectedItem().Path()} }

// In the parent's Update:
case fileSelectedMsg:
    return m, m.preview.Load(msg.path)
```

---

## Best practices

1. **uikit first, bubbles second, hand-roll last** — and promote anything reused twice into uikit.
2. **One responsibility per component** — a component owns its state, key handling, and render; the parent owns layout and routing.
3. **Pass explicit width/height down** — every render fn takes its dimensions as arguments (the uikit layout primitives hand them to you); nothing reads `m.width` from the middle of a subtree.
4. **Lazy-init** anything expensive; return its load as a `Cmd`, never block in `Update`.
5. **Communicate via typed `tea.Msg`s**, not cross-component pointers.
6. **Style only through `uikit.TUI`** — no inline `lipgloss.NewStyle()`, no per-component `ui/` package.

## Dependencies (v2)

**Core (always):**
```
charm.land/bubbletea/v2
charm.land/bubbles/v2     // list, table, textinput, textarea, viewport, progress, spinner, help, key
charm.land/lipgloss/v2
```

**Used in this repo:**
```
github.com/lrstanley/bubblezone/v2   // mouse zone hit-testing
github.com/charmbracelet/glamour     // markdown — via uikit.RenderMarkdown only
github.com/charmbracelet/harmonica   // spring physics for progress/animation
```

**Add deliberately if a feature needs it** (not currently imported): `chroma/v2` (syntax highlighting), a dedicated fuzzy-finder, etc. Prefer the bubbles `list` filter before pulling in a new finder.
