# huh v2 — Theming & Keymaps

## Built-in themes (v2: pass `isDark bool`)

```go
isDark := lipgloss.HasDarkBackground() // standalone; in a TUI track tea.BackgroundColorMsg

form.WithTheme(huh.ThemeCharm(isDark))
//             huh.ThemeDracula(isDark)
//             huh.ThemeCatppuccin(isDark)
//             huh.ThemeBase(isDark)
//             huh.ThemeBase16(isDark)
```

Each built-in returns `*huh.Styles`. The `isDark` parameter is **required in v2** (v1 took none) — it's the single most common migration break.

> **sci-go:** don't set themes inline. Styling flows from the `uikit.TUI` singleton; `uikit.RunForm` applies it. No inline `lipgloss.NewStyle()` outside `internal/uikit/`.

## Theme is an interface

```go
type Theme interface {
    Theme(isDark bool) *Styles
}

type ThemeFunc func(isDark bool) *Styles // satisfies Theme
```

So a custom theme is just a `func(isDark bool) *huh.Styles`:

```go
func myTheme(isDark bool) *huh.Styles {
    s := huh.ThemeBase(isDark)          // start from a base, then tweak
    accent := lipgloss.Color("212")
    s.Focused.Title = s.Focused.Title.Foreground(accent).Bold(true)
    s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(accent)
    s.Focused.Base = s.Focused.Base.BorderForeground(accent)
    return s
}

form.WithTheme(huh.ThemeFunc(myTheme)) // wrap the func to satisfy Theme
```

## Styles structure

```go
type Styles struct {
    Form           FormStyles   // { Base lipgloss.Style }
    Group          GroupStyles
    FieldSeparator lipgloss.Style
    Blurred        FieldStyles  // styles when field is NOT focused
    Focused        FieldStyles  // styles when field IS focused
    Help           help.Styles
}

type FieldStyles struct {
    Base           lipgloss.Style
    Title          lipgloss.Style
    Description    lipgloss.Style
    ErrorIndicator lipgloss.Style
    ErrorMessage   lipgloss.Style
    // Select / MultiSelect:
    SelectSelector lipgloss.Style // cursor/selection indicator
    Option         lipgloss.Style
    NextIndicator  lipgloss.Style
    PrevIndicator  lipgloss.Style
    // …plus MultiSelect cursor/checkbox, Confirm button styles, etc.
}
```

Always edit `Focused` **and** `Blurred` if you want consistent appearance across focus states. All values are Lip Gloss **v2** styles — see the `lipgloss` skill.

## Keymaps

The form keymap is a struct of per-field keymaps:

```go
type KeyMap struct {
    Quit key.Binding
    Confirm     ConfirmKeyMap
    FilePicker  FilePickerKeyMap
    Input       InputKeyMap     // { AcceptSuggestion, Next, Prev, Submit }
    MultiSelect MultiSelectKeyMap
    Note        NoteKeyMap
    Select      SelectKeyMap
    Text        TextKeyMap      // { Next, Prev, NewLine, Editor, Submit }
}
```

Customize from the default and apply:

```go
km := huh.NewDefaultKeyMap()
km.Quit = key.NewBinding(                  // charm.land/bubbles/v2/key
    key.WithKeys("ctrl+c", "esc"),
    key.WithHelp("esc", "quit"),
)
km.Select.Up = key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up"))

form.WithKeyMap(km)
```

- `key` is `charm.land/bubbles/v2/key` (v2) — matches the `bubbletea` skill's binding pattern (`key.Matches`).
- When **embedding**, render the active bindings via `form.Help().ShortHelpView(form.KeyBinds())` so your footer reflects custom keys.
- Set `WithShowHelp(false)` on the form if your host draws its own help bar (avoid double help).
