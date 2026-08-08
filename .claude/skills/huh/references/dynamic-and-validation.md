# huh v2 — Dynamic Forms, Validation, Accessors

## Dynamic fields (`*Func` + binding)

Every static property has a `*Func` twin that recomputes when a **binding** changes. The binding (`any`) is what huh watches; when its value differs from the last render, the function re-runs (and results are cached otherwise). Pass a pointer to the dependency.

```go
var country string
var state string

huh.NewForm(huh.NewGroup(
    huh.NewSelect[string]().
        Title("Country").
        Options(huh.NewOptions("United States", "Canada", "Mexico")...).
        Value(&country),

    huh.NewSelect[string]().
        Value(&state).
        Height(8).
        // Title depends on which country is chosen:
        TitleFunc(func() string {
            switch country {
            case "Canada": return "Province"
            default:       return "State"
            }
        }, &country).                      // ← binding: recompute when country changes
        // Options fetched per-country (huh caches between renders):
        OptionsFunc(func() []huh.Option[string] {
            return huh.NewOptions(fetchStates(country)...)
        }, &country),
))
```

**Why the binding matters:** without `&country`, `OptionsFunc` re-runs on *every* keystroke/render — if it calls an API, you'll hammer it. The binding scopes recomputation to "only when `country` changed."

`*Func` variants exist for: `TitleFunc`, `DescriptionFunc`, `OptionsFunc`, `PlaceholderFunc`, `SuggestionsFunc`.

## Conditional groups (branching)

Hide an entire page based on prior answers:

```go
var hasPet bool

huh.NewForm(
    huh.NewGroup(huh.NewConfirm().Title("Do you have a pet?").Value(&hasPet)),
    huh.NewGroup(
        huh.NewInput().Title("Pet's name"),
    ).WithHideFunc(func() bool { return !hasPet }), // skipped when no pet
)
```

`WithHide(bool)` is the static version; `WithHideFunc(func() bool)` re-evaluates as the form progresses.

## Validation

`Validate(func(T) error)` — return non-nil to mark the field erroneous and block advancing. The error message renders under the field.

```go
huh.NewInput().Title("Email").
    Validate(func(s string) error {
        if !strings.Contains(s, "@") {
            return errors.New("must be a valid email")
        }
        return nil
    })

// Confirm validation (e.g. force acceptance):
huh.NewConfirm().Title("Accept terms?").
    Validate(func(v bool) error {
        if !v { return errors.New("you must accept to continue") }
        return nil
    })

// MultiSelect: enforce a minimum
huh.NewMultiSelect[string]().Title("Pick ≥2").
    Validate(func(sel []string) error {
        if len(sel) < 2 { return errors.New("choose at least two") }
        return nil
    })
```

Field validators run per-field; `form.Errors()` aggregates all current errors (handy for a custom banner when embedding).

## Accessors — custom get/set

When binding a plain `*T` isn't enough (e.g. value lives in a struct, a config object, or needs transforming), implement `Accessor[T]`:

```go
type Accessor[T any] interface {
    Get() T
    Set(value T)
}
```

Built-ins:

```go
// PointerAccessor wraps a *T (this is what .Value() uses under the hood):
acc := huh.NewPointerAccessor(&cfg.Name)
huh.NewInput().Title("Name").Accessor(acc)

// EmbeddedAccessor[T] holds the value internally (read via the field's GetValue):
```

Custom accessor that writes into a struct field with a side effect:

```go
type nameAccessor struct{ cfg *Config }
func (a nameAccessor) Get() string  { return a.cfg.Name }
func (a nameAccessor) Set(v string) { a.cfg.Name = strings.TrimSpace(v) }

huh.NewInput().Title("Name").Accessor(nameAccessor{cfg: c})
```

## Building options from data — use the `lo` skill

`huh.NewOptions(vals...)` covers plain values. To project **structs** into `huh.Option[T]`, reach for `lo.Map` (see the `lo` skill):

```go
import "github.com/samber/lo"

type Repo struct{ Name, ID string }

opts := lo.Map(repos, func(r Repo, _ int) huh.Option[string] {
    return huh.NewOption(r.Name, r.ID) // label = Name, value = ID
})

huh.NewSelect[string]().Title("Repo").Options(opts...).Value(&repoID)
```

Pre-select with `.Selected(true)`:

```go
opts := lo.Map(tags, func(t Tag, _ int) huh.Option[string] {
    return huh.NewOption(t.Label, t.Key).Selected(t.Default)
})
```

Filter the source first with `lo.Filter`, dedupe with `lo.Uniq`, etc. — keep option-building as a clean `lo` pipeline rather than manual `for`/`append`.

## Accessible mode

Screen-reader mode is **form-level only** in v2 (field-level `WithAccessible` was removed). Wire it to an env var so users opt in:

```go
form.WithAccessible(os.Getenv("ACCESSIBLE") != "")
```

Accessible forms drop the TUI for plain sequential prompts with dictation-friendly output.
