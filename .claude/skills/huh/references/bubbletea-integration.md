# huh v2 inside a Bubble Tea v2 app

A `huh.Form` **is** a `tea.Model` (the exported `huh.Model` interface). Embed it as a sub-model when part of your TUI needs form-like input. This is the path the `bubbletea` skill expects; read that skill's golden-rules + layout sections for the host's sizing.

> **sci-go boundary:** in this repo you almost never hand-embed a form — you call `uikit.RunForm` / `uikit.Input` / `uikit.InputInto` / `uikit.Select`, which own the `huh` lifecycle and the `uikit.TUI` theme singleton. The pattern below is what those wrappers implement; extend `uikit` rather than embedding `huh.Form` ad hoc in `internal/tui/*`.

## Minimal embed pattern

```go
import (
    tea "charm.land/bubbletea/v2"
    "charm.land/huh/v2"
)

type Model struct {
    form      *huh.Form
    hasDarkBg bool
}

func NewModel() Model {
    return Model{
        form: huh.NewForm(
            huh.NewGroup(
                huh.NewSelect[string]().
                    Key("class").
                    Options(huh.NewOptions("Warrior", "Mage", "Rogue")...).
                    Title("Choose your class"),
                huh.NewInput().Key("name").Title("Character name"),
            ),
        ).
            WithWidth(45).
            WithShowHelp(false). // host renders its own help/footer
            WithShowErrors(false),
    }
}

func (m Model) Init() tea.Cmd { return m.form.Init() }
```

## Update — cast the result back to `*huh.Form`

`form.Update` returns `(huh.Model, tea.Cmd)`. **Always type-assert back to `*huh.Form`** before storing:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.BackgroundColorMsg:
        m.hasDarkBg = msg.IsDark()   // v2: track dark-mode for theming
    case tea.KeyPressMsg:            // v2 key type (not tea.KeyMsg)
        switch msg.String() {
        case "ctrl+c":
            return m, tea.Interrupt
        case "esc":
            return m, tea.Quit
        }
    }

    var cmds []tea.Cmd
    form, cmd := m.form.Update(msg)
    if f, ok := form.(*huh.Form); ok {
        m.form = f
        cmds = append(cmds, cmd)
    }

    if m.form.State == huh.StateCompleted {
        cmds = append(cmds, tea.Quit) // or transition to your next state
    }
    return m, tea.Batch(cmds...)
}
```

`State` cycles `StateNormal → StateCompleted | StateAborted`. Read answers with the typed getters once completed.

## View — return `tea.View` (v2 declarative)

In Bubble Tea v2 the host's `View()` returns `tea.View`, built with `tea.NewView(string)`. Declare alt-screen / mouse on that value rather than via program options:

```go
func (m Model) View() tea.View {
    switch m.form.State {
    case huh.StateCompleted:
        return tea.NewView(fmt.Sprintf(
            "You are a %s named %s\n",
            m.form.GetString("class"), m.form.GetString("name")))
    default:
        // m.form.View() returns a string; compose it with lipgloss as usual.
        body := lipgloss.JoinHorizontal(lipgloss.Left,
            m.form.View(),
            m.sidebar())
        footer := m.form.Help().ShortHelpView(m.form.KeyBinds())
        v := tea.NewView(body + "\n\n" + footer)
        // v.AltScreen = true   // declare host options here if needed
        return v
    }
}
```

- `m.form.View()` is a **string** — compose it inside your layout with Lip Gloss (`JoinHorizontal`, panels, etc.). Follow the `bubbletea` skill's golden rules (account for borders, truncate, weight-based sizing).
- `m.form.Help()` + `m.form.KeyBinds()` give you the form's help/keybindings to render in your own footer (the example sets `WithShowHelp(false)` so the host owns the footer).
- `m.form.Errors()` returns current validation errors for a custom error banner.

## Dark-mode / theming in a TUI

Themes take an `isDark bool` in v2. Detect it from `tea.BackgroundColorMsg` (fired on startup), store it, and rebuild the theme when it changes:

```go
case tea.BackgroundColorMsg:
    m.hasDarkBg = msg.IsDark()
    m.form = m.form.WithTheme(huh.ThemeCharm(m.hasDarkBg))
```

Outside a TUI use `lipgloss.HasDarkBackground()` directly. (sci-go: theme comes from the `uikit.TUI` singleton — don't build inline themes.)

## View hooks

`WithViewHook` lets you mutate the host `tea.View` huh produces (alt-screen, mouse mode, etc.) — useful when running a form standalone but needing program-level toggles:

```go
form.WithViewHook(func(v tea.View) tea.View {
    v.AltScreen = true
    return v
})
```

`WithProgramOptions(...tea.ProgramOption)` applies when you call `form.Run()` standalone (it builds its own `tea.Program`). When **embedded**, the host program owns options — use the host's `View`/program instead.

## Multi-form layouts

Form-level `WithLayout` arranges multiple **groups** on screen at once:

```go
form.WithLayout(huh.LayoutDefault)      // one group at a time (default)
form.WithLayout(huh.LayoutStack)        // stacked
form.WithLayout(huh.LayoutColumns(2))   // n columns
form.WithLayout(huh.LayoutGrid(2, 3))   // rows × columns
```

## Gotchas

- **Cast `Update` result** to `*huh.Form` every time — skipping it silently freezes the form.
- **v2 message types**: `tea.KeyPressMsg` (not `tea.KeyMsg`), `tea.MouseClickMsg` etc. — see the `bubbletea` skill's v2 delta table.
- **Don't double-render help**: either `WithShowHelp(true)` (form draws it) or render `form.Help()`/`KeyBinds()` yourself — not both.
- **Quit on completion**: the form won't exit your program; watch `State == StateCompleted` (and `StateAborted`) and act.
- **`ctrl+c`** in the example maps to `tea.Interrupt`; `esc`/`q` to `tea.Quit`. Pick deliberately so the form's own keymap doesn't conflict.
