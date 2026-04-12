---
name: lo
description: Write modern, expressive Go using samber/lo — the generic functional toolkit. Use when writing transforms (Map, Filter, Reduce, GroupBy, KeyBy), set operations (Intersect, Difference, Union), error-aware variants (*Err), or replacing manual for+append loops. Covers Python/JS-to-Go idiom translation. Invoke for any slice/map transform, collection pipeline, or when CLAUDE.md says "use lo".
version: 1.0.0
license: MIT
---

# samber/lo — Modern Go Transforms

Write expressive, generic Go using `samber/lo` instead of verbose manual loops. This skill helps Python/JS developers write idiomatic Go that reads like the functional code they already know.

## When to Use This Skill

- Writing any slice transform (map, filter, reduce, group, sort, dedupe)
- Building maps from slices or transforming map keys/values
- Set operations (intersect, difference, union, membership)
- Replacing `for` + `append` loops flagged by semgrep (`.semgrep/go-modern.yml`)
- Translating Python/JS idioms to Go (`list comprehensions`, `Array.map`, `dict comprehensions`)
- Error-aware pipelines where callbacks can fail (`*Err` variants)
- Any code where CLAUDE.md says "use `lo`"

## Import

```go
import "github.com/samber/lo"
```

Parallel variants: `lop "github.com/samber/lo/parallel"`
Mutable variants: `lom "github.com/samber/lo/mutable"`

## Decision Framework

```
Need to transform a slice?
├── Same type, simple check → slices.Contains / slices.Sort (stdlib)
├── Different output type  → lo.Map
├── Filter + transform     → lo.FilterMap (one pass)
├── Transform → flatten    → lo.FlatMap
├── Group by key           → lo.GroupBy
├── Slice → map            → lo.KeyBy (unique keys) or lo.SliceToMap (custom k/v)
├── Deduplicate            → lo.Uniq / lo.UniqBy
├── Accumulate to value    → lo.Reduce
├── Any callback can error → use the *Err variant (lo.MapErr, lo.FilterErr, etc.)
└── I/O-bound parallelism  → lop.Map / lop.ForEach

Need to transform a map?
├── Filter entries    → lo.PickBy / lo.OmitBy
├── Transform keys    → lo.MapKeys
├── Transform values  → lo.MapValues
├── Transform both    → lo.MapEntries
├── Map → slice       → lo.MapToSlice
├── Merge maps        → lo.Assign
└── Subset by keys    → lo.PickByKeys / lo.OmitByKeys

Need set operations?
├── Membership  → lo.Contains / lo.ContainsBy
├── Intersection → lo.Intersect
├── Difference   → lo.Difference (returns left-only, right-only)
├── Union        → lo.Union
└── Remove items → lo.Without / lo.WithoutBy
```

## Core Patterns (with Python/JS equivalents)

### Slice Transforms

See `references/slice-transforms.md` for the complete catalog.

**Map** — transform every element (Python: `[f(x) for x in xs]`, JS: `xs.map(f)`)
```go
names := lo.Map(users, func(u User, _ int) string {
    return u.Name
})
```

**Filter** — keep matching elements (Python: `[x for x in xs if pred(x)]`, JS: `xs.filter(pred)`)
```go
active := lo.Filter(users, func(u User, _ int) bool {
    return u.Active
})
```

**FilterMap** — filter + transform in one pass (Python: `[f(x) for x in xs if pred(x)]`)
```go
emails := lo.FilterMap(users, func(u User, _ int) (string, bool) {
    return u.Email, u.Email != ""
})
```

**Reduce** — accumulate to single value (Python: `functools.reduce`, JS: `xs.reduce`)
```go
total := lo.Reduce(items, func(sum int, item Item, _ int) int {
    return sum + item.Price
}, 0)
```

**GroupBy** — group into buckets (Python: `itertools.groupby` / `defaultdict(list)`)
```go
byDept := lo.GroupBy(employees, func(e Employee) string {
    return e.Department
})
// map[string][]Employee
```

**KeyBy** — index by unique key (Python: `{x.id: x for x in xs}`)
```go
byID := lo.KeyBy(users, func(u User) int {
    return u.ID
})
// map[int]User
```

**FlatMap** — transform + flatten (Python: `[y for x in xs for y in f(x)]`, JS: `xs.flatMap(f)`)
```go
allTags := lo.FlatMap(posts, func(p Post, _ int) []string {
    return p.Tags
})
```

### Map Transforms

See `references/map-transforms.md` for the complete catalog.

```go
// Transform values
upper := lo.MapValues(m, func(v string, _ string) string {
    return strings.ToUpper(v)
})

// Filter entries
admins := lo.PickBy(users, func(_ string, u User) bool {
    return u.Role == "admin"
})

// Map → slice
lines := lo.MapToSlice(scores, func(name string, score int) string {
    return fmt.Sprintf("%s: %d", name, score)
})

// Merge maps (last wins)
merged := lo.Assign(defaults, overrides)
```

### Set Operations

See `references/set-ops.md` for the complete catalog.

```go
common := lo.Intersect(listA, listB)                    // elements in both
onlyA, onlyB := lo.Difference(listA, listB)             // symmetric diff
all := lo.Union(listA, listB, listC)                     // deduplicated merge
cleaned := lo.Without(items, "banned1", "banned2")       // remove specific values
lo.Contains(slice, value)                                // membership test
lo.Every(haystack, needles)                              // all needles present?
lo.Some(haystack, needles)                               // any needle present?
```

### Error-Aware Pipelines

See `references/error-handling.md` for all `*Err` variants.

When callbacks touch I/O or can fail, use `*Err` variants — they short-circuit on first error:
```go
results, err := lo.MapErr(urls, func(url string, _ int) (Response, error) {
    return http.Get(url)
})

valid, err := lo.FilterErr(records, func(r Record, _ int) (bool, error) {
    return r.Validate()
})
```

### Utility Helpers

```go
// Ternary (Python: `a if cond else b`, JS: `cond ? a : b`)
label := lo.Ternary(count == 1, "item", "items")

// Coalesce — first non-zero value (Python: `next(x for x in xs if x)`, JS: `a ?? b ?? c`)
name, ok := lo.Coalesce(user.Nickname, user.FullName, "Anonymous")

// Chunk (Python: chunked from more-itertools)
batches := lo.Chunk(allItems, 100)

// Compact — remove zero values (Python: `[x for x in xs if x]`)
nonEmpty := lo.Compact([]string{"", "foo", "", "bar"})

// Pointer helpers
ptr := lo.ToPtr("hello")         // *string
val := lo.FromPtrOr(nilPtr, "")  // safe deref with default

// Frequency map (Python: `Counter(xs)`)
counts := lo.CountValues(tags)   // map[string]int

// Build set from slice
set := lo.Keyify(ids)            // map[int]struct{}

// Find first match
user, ok := lo.Find(users, func(u User) bool { return u.ID == targetID })

// Deduplicate
unique := lo.Uniq(tags)
uniqueBy := lo.UniqBy(users, func(u User) int { return u.ID })
```

## stdlib vs lo — When to Use Which

| Task | Use | Example |
|---|---|---|
| Sort slice | `slices.Sort` / `slices.SortFunc` | `slices.SortFunc(users, func(a, b User) int { return cmp.Compare(a.Name, b.Name) })` |
| Clone slice | `slices.Clone` | `copy := slices.Clone(original)` |
| Concat slices | `slices.Concat` | `all := slices.Concat(a, b, c)` |
| Simple contains | `slices.Contains` | `slices.Contains(ids, 42)` |
| Sorted map keys | `slices.Sorted(maps.Keys(m))` | — |
| Clone bytes | `bytes.Clone` | — |
| Map/Filter/Reduce | **`lo`** | stdlib has none of these |
| GroupBy/KeyBy | **`lo`** | stdlib has none of these |
| Set ops (intersect/diff) | **`lo`** | stdlib has none of these |
| Find with predicate | **`lo.Find`** | `slices.Contains` only checks equality |

**Rule of thumb:** if stdlib has it, use stdlib. If not (Map, Filter, GroupBy, KeyBy, Find, Reduce, Chunk, set ops), use `lo`. Never hand-roll what either provides.

## Performance Notes

- `lo.Map` is ~4% slower than a hand-written `for` loop — negligible for all practical purposes.
- `lo.Map` is ~7x faster than reflection-based libraries (e.g., go-funk).
- `lop.Map` (parallel) adds goroutine overhead — only use for I/O-bound callbacks, not CPU-bound transforms.
- All `lo` functions are allocation-friendly — same profile as hand-written equivalents.

## Semgrep Integration

This project enforces `lo` usage via `.semgrep/go-modern.yml`. Run `just lint-style` to check. Common flags:
- Manual `for` + `append` with transform → use `lo.Map`
- Manual `for` + `if` + `append` → use `lo.Filter`
- Manual map building from slice → use `lo.KeyBy` or `lo.SliceToMap`
- Manual `for` + counter → use `lo.CountBy` or `lo.CountValues`
- `sort.Slice` / `sort.Strings` → use `slices.Sort` / `slices.SortFunc`

## Reference Files

- `references/slice-transforms.md` — Complete slice function catalog with signatures and examples
- `references/map-transforms.md` — Complete map function catalog
- `references/set-ops.md` — Set operations, search, and membership helpers
- `references/error-handling.md` — All `*Err` variants and error utilities
- `references/python-js-rosetta.md` — Side-by-side Python/JS → Go+lo translations
- `references/concurrency.md` — Parallel variants, channels, async, retry, debounce
- `references/misc.md` — Tuples, math, strings, time, pointer helpers, conditionals
