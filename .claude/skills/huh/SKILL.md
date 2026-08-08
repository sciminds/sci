---
name: huh
description: Build interactive terminal forms and prompts in Go with charm.land/huh v2. Use for input/select/multiselect/confirm/text/note/filepicker fields, grouped multi-page forms, dynamic (Func-based) forms, validation, accessible mode, theming, and embedding forms inside a Bubble Tea v2 app. Pairs with the bubbletea and lo skills.
version: 1.0.0
license: MIT
---

# huh? — Terminal Forms in Go

Build interactive forms and prompts with [`charm.land/huh/v2`](https://pkg.go.dev/charm.land/huh/v2). Forms are organized into **groups** (pages) of **fields** (`Input`, `Text`, `Select`, `MultiSelect`, `Confirm`, `Note`, `FilePicker`). Each field is a `tea.Model`, so a whole `huh.Form` drops directly into a Bubble Tea v2 app.

Pairs with the project's **`bubbletea`** skill (the v2 TUI host + layout rules) and **`lo`** skill (building option slices from data). For styling primitives see **`lipgloss`**.

## When to Use This Skill

- Prompting for input outside or inside a TUI (single field or full multi-page form)
- Select / multi-select pickers, confirmations, multi-line text, file pickers
- Dynamic forms whose titles/options recompute from earlier answers
- Field validation and error display
- Accessible (screen-reader) prompts
- Embedding a form as a sub-model in a Bubble Tea v2 application
- Standalone background spinners (`charm.land/huh/v2/spinner`) after a form submits

## Version & compatibility (read first)

This skill targets **huh v2** (`charm.land/huh/v2`, latest `v2.0.3` as of 2026-05). huh v2 is wired to the **same v2 Charm stack the `bubbletea` skill mandates** — so it's drop-in compatible with `sci-go`:

```
charm.land/huh/v2         // requires:
charm.land/bubbletea/v2   //   v2.0.6+
charm.land/bubbles/v2     //   v2.1.0+
charm.land/lipgloss/v2    //   v2.0.3+
```

```go
import (
    "charm.land/huh/v2"
    "charm.land/huh/v2/spinner"
    tea "charm.land/bubbletea/v2"
    "charm.land/lipgloss/v2"
)
```

**Never mix v1 (`github.com/charmbracelet/huh`) and v2** — import-cycle / type-mismatch errors result. Key v1→v2 deltas (full table in `references/upgrade-v1-to-v2.md`):

| v1 | v2 |
|---|---|
| `github.com/charmbracelet/huh` | `charm.land/huh/v2` |
| `huh.ThemeCharm()` | `huh.ThemeCharm(isDark bool)` — takes dark-mode flag |
| `Theme` is a struct | `Theme` is an interface; `ThemeFunc(isDark) *Styles` |
| field-level `WithAccessible()` | removed — **form-level only** (`Form.WithAccessible`) |
| `github.com/charmbracelet/huh/accessibility` pkg | gone — use `Form.WithAccessible(true)` |
| `View() string` (host model) | v2 host returns `tea.View` (declarative; see integration ref) |

## Project convention — route forms through `uikit` (sci-go)

> **CRITICAL for this repo.** The `bubbletea` skill mandates: **`huh` forms must go through `uikit`.** Use `uikit.RunForm` / `uikit.Input` / `uikit.InputInto` / `uikit.Select`; confirmations via `cmdutil.Confirm` / `ConfirmYes`. **Never call `huh.Form.Run()` (or a field's `.Run()`) directly** in app code. No inline `lipgloss.NewStyle()` outside `internal/uikit/` — themes come from the `uikit.TUI` singleton. The raw `huh` API below is what `uikit` wraps; build/extend the wrappers, don't bypass them.

Outside sci-go, calling `.Run()` directly is the normal standalone path.

## Quick start (standalone)

```go
var (
    name    string
    topping []string
    spicy   bool
)

form := huh.NewForm(
    huh.NewGroup(
        huh.NewInput().Title("Name").Value(&name).
            Validate(func(s string) error {
                if s == "" { return errors.New("required") }
                return nil
            }),
        huh.NewMultiSelect[string]().
            Title("Toppings").Limit(4).
            Options(huh.NewOptions("Lettuce", "Tomato", "Cheese", "Onion")...).
            Value(&topping),
        huh.NewConfirm().Title("Make it spicy?").Value(&spicy),
    ),
)

if err := form.Run(); err != nil { // standalone only; in sci-go use uikit.RunForm
    log.Fatal(err)
}
```

`Value(&v)` binds the answer to your variable. **`NewSelect`/`NewMultiSelect` are generic** — `NewSelect[string]()`, `NewSelect[int]()`, etc. Option values can be any `comparable` type.

## Decision framework

```
Need to gather input?
├── One quick value, no TUI host    → field.Run()  (or uikit.Input in sci-go)
├── Several fields / multi-page     → huh.NewForm(huh.NewGroup(...), ...)
├── Just a yes/no                    → huh.NewConfirm()  (uikit.Confirm in sci-go)
├── Pick one from list               → huh.NewSelect[T]()
├── Pick many from list              → huh.NewMultiSelect[T]().Limit(n)
├── Long / multi-line text           → huh.NewText().Lines(n)  (or .ExternalEditor(true))
├── Static info / no input           → huh.NewNote()
├── Choose a file/dir                → huh.NewFilePicker()
├── Options depend on prior answers  → .TitleFunc / .OptionsFunc(fn, &binding)
├── Hide a whole page conditionally  → group.WithHideFunc(func() bool)
└── Inside a Bubble Tea v2 app       → embed form as a sub-model (see integration ref)

Background work after submit?        → spinner.New().Title(..).Action(..).Run()
```

## Reading results

Bind with `Value(&v)` (simplest), or tag fields with `Key("id")` and read post-completion:

```go
form.GetString("class")   // typed getters
form.GetInt("level")
form.GetBool("done")
form.Get("custom")        // any
```

`form.State` is `StateNormal` / `StateCompleted` / `StateAborted`. Aborting returns `huh.ErrUserAborted`; timeouts return `huh.ErrTimeout`.

## Common form-level options (chainable, return `*Form`)

```go
form.
    WithTheme(huh.ThemeCharm(lipgloss.HasDarkBackground())). // isDark flag required in v2
    WithAccessible(os.Getenv("ACCESSIBLE") != "").           // screen-reader mode (form-level only)
    WithWidth(60).WithHeight(20).
    WithShowHelp(true).WithShowErrors(true).
    WithKeyMap(huh.NewDefaultKeyMap()).
    WithLayout(huh.LayoutColumns(2)).                        // Default/Stack/Columns(n)/Grid(r,c)
    WithTimeout(30 * time.Second).
    WithProgramOptions(/* tea v2 ProgramOptions */).         // standalone .Run() only
    WithOutput(w).WithInput(r)
```

## Reference files (load as needed)

- **`references/fields.md`** — every field type and its full builder-method surface (Input, Text, Select, MultiSelect, Confirm, Note, FilePicker), plus `Option`/`NewOptions`. **Read when configuring any non-trivial field.**
- **`references/bubbletea-integration.md`** — embedding a `huh.Form` inside a Bubble Tea v2 app: `Update` casting, `tea.View` host, dark-bg detection, `WithViewHook`, layouts, and the sci-go `uikit.RunForm` boundary. **Read for any in-TUI form.**
- **`references/dynamic-and-validation.md`** — dynamic `*Func` fields with `binding`, validation patterns, `Accessor` (custom get/set), conditional groups, and building `Option` slices from data with the `lo` skill.
- **`references/theming-keymaps.md`** — `ThemeFunc`/`Styles`/`FieldStyles` structure, the 5 built-in themes, custom themes, and per-field/global keymap customization.
- **`references/spinner.md`** — the standalone `charm.land/huh/v2/spinner` package for background work.
- **`references/upgrade-v1-to-v2.md`** — complete v1→v2 migration checklist and breaking changes.

## Best practices

1. **Stay on the v2 stack** — `charm.land/*/v2` everywhere; never import v1 huh/bubbletea/lipgloss alongside it.
2. **In sci-go, go through `uikit`** — never `huh.Form.Run()` directly; extend the wrappers.
3. **Pass `isDark` to themes** — `huh.ThemeCharm(lipgloss.HasDarkBackground())`; in a TUI, track it from `tea.BackgroundColorMsg`.
4. **Accessible mode is form-level** — wire `WithAccessible` to an env var/flag; field-level `WithAccessible` no longer exists.
5. **Validate at the field** — `.Validate(func(T) error)` marks the field and blocks submit; cheaper than post-hoc checks.
6. **Dynamic fields need a `binding`** — `.OptionsFunc(fn, &dep)` recomputes only when `&dep` changes (caching); omit it and you'll hit APIs every keystroke.
7. **Build options from data with `lo.Map`** — `huh.NewOptions(...)` for plain values; `lo.Map` to project structs into `huh.Option[T]`.
8. **Embedded forms**: cast the `Update` result back to `*huh.Form`, quit on `StateCompleted`, and return a `tea.View` from the host (v2).

## External resources

- [huh v2 docs](https://pkg.go.dev/charm.land/huh/v2)
- [huh v2 upgrade guide](https://github.com/charmbracelet/huh/blob/main/UPGRADE_GUIDE_V2.md)
- [Bubble Tea integration example](https://github.com/charmbracelet/huh/blob/main/examples/bubbletea/main.go)
- [examples/ directory](https://github.com/charmbracelet/huh/tree/main/examples) — dynamic, conditional, layout, theme, filepicker, ssh-form, spinner
