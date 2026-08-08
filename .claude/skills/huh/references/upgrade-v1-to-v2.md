# huh v1 → v2 Migration

huh v2 moves to the `charm.land` vanity domain with a `/v2` suffix and aligns with Bubble Tea / Lip Gloss / Bubbles v2. Most changes are mechanical import rewrites; a few are semantic.

## Update dependencies

```bash
go get charm.land/huh/v2@latest
go get charm.land/bubbletea/v2@latest
go get charm.land/lipgloss/v2@latest
go get charm.land/bubbles/v2@latest
go mod tidy
```

## Import path rewrites

| v1 | v2 |
|----|----|
| `github.com/charmbracelet/huh` | `charm.land/huh/v2` |
| `github.com/charmbracelet/huh/spinner` | `charm.land/huh/v2/spinner` |
| `github.com/charmbracelet/huh/accessibility` | **removed** — use `Form.WithAccessible` |
| `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| `github.com/charmbracelet/bubbles` | `charm.land/bubbles/v2` |

## Semantic breaking changes

### 1. Themes take `isDark bool`
```go
// v1
form.WithTheme(huh.ThemeCharm())
// v2
form.WithTheme(huh.ThemeCharm(lipgloss.HasDarkBackground()))
```
All five built-ins (`ThemeCharm/Dracula/Catppuccin/Base/Base16`) now require the flag.

### 2. `Theme` is now an interface
```go
// v1: Theme was a struct you filled in.
// v2:
type Theme interface { Theme(isDark bool) *Styles }
type ThemeFunc func(isDark bool) *Styles  // wrap a func to satisfy Theme
```
Custom themes become `func(isDark bool) *huh.Styles`; wrap with `huh.ThemeFunc(...)`.

### 3. Field-level `WithAccessible()` removed
Accessible mode is **form-level only** now. Remove `WithAccessible` from `Input`, `Text`, `Select`, `MultiSelect`, `Confirm`, `Note`, `FilePicker`; keep `Form.WithAccessible(true)`.

### 4. Bubble Tea v2 types everywhere
`Init/Update/Focus/Blur` now use `charm.land/bubbletea/v2` `Cmd`/`Msg`/`Model`. `KeyBinds()` returns `charm.land/bubbles/v2/key.Binding`. `WithProgramOptions` takes v2 program options. The host model's `View()` returns `tea.View` (see bubbletea-integration.md). Mechanical for most code; your IDE handles most of it.

### 5. Lip Gloss v2 types in custom themes/styles
`lipgloss.Style`, colors, and `Position` (e.g. `WithButtonAlignment(lipgloss.Left)`) are all v2 types now. API is largely identical; the import path/internal types changed.

## New in v2

- **`WithViewHook(func(tea.View) tea.View)`** — mutate the view (alt-screen, mouse mode) before render. Available on both `Form` and `spinner.Spinner`.
- **`Model` type exported** — `var _ tea.Model = (*huh.Model)(nil)`; better type safety when embedding.
- **`(*MultiSelect[T]).Width()`** — read a field's computed width.

## Checklist

- [ ] Bump go.mod deps to v2, `go mod tidy`
- [ ] Rewrite all `github.com/charmbracelet/*` imports → `charm.land/*/v2`
- [ ] Add `isDark` to every theme call
- [ ] Delete field-level `WithAccessible` calls (keep form-level)
- [ ] Delete `huh/accessibility` imports
- [ ] Convert custom themes to `ThemeFunc` signature
- [ ] Verify no v1 deps linger: `go list -m all | grep charmbracelet`
- [ ] `go build ./...` and run tests

## Common issues

- **Import cycle / type mismatch on `tea.Model`** → a dependency is still on v1. `go list -m all | grep -E 'charmbracelet|charm.land'` and bump the straggler.
- **Theme signature error** → forgot the `isDark bool` argument.
