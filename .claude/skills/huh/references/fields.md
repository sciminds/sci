# huh v2 — Field Reference

Every field is created with `huh.NewX()`, configured with chained builder methods (each returns the field for chaining), and added to a `huh.NewGroup(...)`. All fields share a common set of binding/identity methods plus their own specifics.

## Shared across all input fields

| Method | Purpose |
|---|---|
| `.Value(*T)` | Bind the answer to a variable |
| `.Accessor(Accessor[T])` | Custom get/set instead of `Value` (see dynamic-and-validation.md) |
| `.Key(string)` | Tag for `form.GetString/GetInt/GetBool/Get` lookup |
| `.Title(string)` / `.TitleFunc(fn, binding)` | Field title (static or dynamic) |
| `.Description(string)` / `.DescriptionFunc(fn, binding)` | Subtitle (static or dynamic) |
| `.Validate(func(T) error)` | Block submit + show error when non-nil |
| `.Run() error` | Run this field alone (standalone; in sci-go use `uikit`) |

`Title`/`Description`/`Options`/`Placeholder`/`Suggestions` each have a `*Func` twin taking `(fn, binding any)` for dynamic recomputation — see `dynamic-and-validation.md`.

`Field` is an interface (embeds the Bubble Tea `Model`): `Focus/Blur() tea.Cmd`, `Error() error`, `KeyBinds() []key.Binding`, `GetKey()`, `GetValue() any`, plus `WithTheme/WithKeyMap/WithWidth/WithHeight/WithPosition` (used by the form, rarely by you).

---

## Input — single-line text

```go
huh.NewInput().
    Title("What's for lunch?").
    Prompt("? ").
    Placeholder("a tasty meal").
    CharLimit(120).
    Suggestions([]string{"pizza", "tacos"}). // tab-complete; or SuggestionsFunc
    Inline(true).                            // title + input on one line
    Validate(isFood).
    Value(&lunch)
```

Password / secret entry:

```go
huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&pw)
// EchoModeNormal (default) | EchoModePassword (mask) | EchoModeNone (hide)
// .Password(true) is shorthand for EchoModePassword.
```

Input-specific: `Prompt`, `CharLimit`, `Suggestions`/`SuggestionsFunc`, `EchoMode`, `Password`, `Placeholder`/`PlaceholderFunc`, `Inline`.

---

## Text — multi-line text

```go
huh.NewText().
    Title("Tell me a story").
    Lines(5).                 // visible rows
    CharLimit(4000).
    ShowLineNumbers(true).
    Placeholder("Once upon a time…").
    Validate(checkForPlagiarism).
    Value(&story)
```

Open the user's `$EDITOR` for long text:

```go
huh.NewText().Title("Commit message").
    ExternalEditor(true).             // enable $EDITOR
    Editor("nvim").                   // override binary (optional)
    EditorExtension("md").            // temp-file extension for syntax (optional)
    Value(&msg)
```

Text-specific: `Lines`, `CharLimit`, `ShowLineNumbers`, `Placeholder`/`PlaceholderFunc`, `ExternalEditor`, `Editor`, `EditorExtension`.

---

## Select[T] — pick one (generic)

```go
huh.NewSelect[string]().
    Title("Pick a country").
    Options(
        huh.NewOption("United States", "US"),
        huh.NewOption("Germany", "DE").Selected(true), // default highlight
        huh.NewOption("Brazil", "BR"),
    ).
    Filtering(true).   // type-to-filter (default on)
    Inline(false).
    Height(8).         // viewport rows before scrolling
    Validate(func(v string) error { … }).
    Value(&country)
```

- **Generic over value type**: `NewSelect[int]()`, `NewSelect[MyEnum]()` — value can be any `comparable`.
- `.Hovered() (T, bool)` — current cursor value (useful when embedding).
- Select-specific: `Options`/`OptionsFunc`, `Filtering`, `Inline`, `Height`.

---

## MultiSelect[T] — pick many (generic)

```go
huh.NewMultiSelect[string]().
    Title("Toppings").
    Options(
        huh.NewOption("Lettuce", "lettuce").Selected(true),
        huh.NewOption("Cheese", "cheese"),
        huh.NewOption("Nutella", "nutella"),
    ).
    Limit(4).            // max selections
    Filterable(true).
    Height(10).
    Value(&toppings)     // Value binds to *[]T
```

- `Value` takes `*[]T`. `Limit(n)` caps selections.
- MultiSelect-specific: `Options`/`OptionsFunc`, `Filterable`, `Filtering`, `Limit`, `Width`, `Height`, `.Hovered()`.

## Options helpers

```go
// Plain values → key == String(value):
huh.NewOptions("Red", "Green", "Blue")          // []Option[string]
huh.NewOptions(1, 20, 9999)                      // []Option[int]

// Distinct label vs value:
huh.NewOption("United States", "US")             // Option[string]{key:"United States", value:"US"}
huh.NewOption("A lot", 2).Selected(true)         // pre-selected

// Spread into Options(...):
.Options(huh.NewOptions("a", "b", "c")...)
```

Build from structs with the **`lo` skill** — see `dynamic-and-validation.md`.

---

## Confirm — yes/no

```go
huh.NewConfirm().
    Title("Would you like 15% off?").
    Affirmative("Yes!").
    Negative("No.").
    Inline(true).
    WithButtonAlignment(lipgloss.Left).   // lipgloss v2 Position
    Validate(func(v bool) error { … }).
    Value(&discount)                       // *bool
```

In sci-go, prefer `cmdutil.Confirm` / `cmdutil.ConfirmYes` over a raw Confirm.

---

## Note — static info (no input)

```go
huh.NewNote().
    Title("Terms of Service").
    Description("Read carefully before continuing.").
    Next(true).               // show a "next" button so it's a real step
    NextLabel("Continue").
    Height(6)
```

`Note` has no value (`GetValue()` is nil). `Skip()` returns true unless `Next(true)`. Description supports basic markdown rendering.

---

## FilePicker — choose a file/dir

```go
huh.NewFilePicker().
    Title("Select a config file").
    CurrentDirectory("/etc").
    ShowHidden(false).
    ShowSize(true).
    ShowPermissions(true).
    FileAllowed(true).
    DirAllowed(false).
    AllowedTypes([]string{".json", ".yaml", ".toml"}).
    Height(12).
    Validate(func(path string) error { … }).
    Value(&path)              // *string (selected path)
```

FilePicker-specific: `CurrentDirectory`, `Cursor`, `Picking`, `ShowHidden`, `ShowSize`, `ShowPermissions`, `FileAllowed`, `DirAllowed`, `AllowedTypes`, `Height`.

---

## Groups — pages of fields

```go
huh.NewGroup(field1, field2, field3).
    Title("Personal details").
    Description("We'll keep this private.").
    WithHideFunc(func() bool { return !needsDetails }). // conditionally skip the whole page
    WithShowHelp(true).
    WithShowErrors(true).
    WithWidth(60).WithHeight(20)
```

A `huh.NewForm(group1, group2, …)` advances group-by-group. Use multiple groups for wizard-style flows; use `WithHideFunc`/`WithHide` to branch.
