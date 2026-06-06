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
└── Parallel + fallible    → errgroup, NOT lop (lop has no error variant — see Gotchas)

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
// Transform values — MapValues/MapKeys callback is (value, key)
upper := lo.MapValues(m, func(v, key string) string {
    return strings.ToUpper(v)
})

// Filter entries — PickBy callback is (key, value)
admins := lo.PickBy(users, func(name string, u User) bool {
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
| Reverse in place | `slices.Reverse` | — |
| Simple contains (equality) | `slices.Contains` | `slices.Contains(ids, 42)` |
| Sorted map keys | `slices.Sorted(maps.Keys(m))` | iterator-based since Go 1.23 |
| Clone bytes | `bytes.Clone` | — |
| Map/Filter/Reduce | **`lo`** | stdlib has none of these |
| GroupBy/KeyBy | **`lo`** | stdlib has none of these |
| Set ops (intersect/diff) | **`lo`** | stdlib has none of these |
| Contains by predicate | **`lo.ContainsBy`** | this project's convention — see note |
| Find value + index by predicate | **`lo.Find`** / **`lo.FindIndexOf`** | returns the element, not just a bool |

**stdlib has predicate helpers too** — `slices.ContainsFunc`, `slices.IndexFunc`, `slices.MaxFunc`, `slices.MinFunc` overlap with `lo.ContainsBy` / `lo.FindIndexOf` / `lo.MaxBy` / `lo.MinBy`. **This project deliberately prefers the `lo` forms for predicate work** — the `.semgrep/go-modern.yml` rules rewrite manual predicate-search loops to `lo.ContainsBy` and `lo.FindIndexOf`, not the `slices.*Func` equivalents, because `lo` returns the matched value (not just a bool/index) and keeps the call style consistent with the rest of the pipeline. Reserve `slices.Contains` (no `Func`) for plain equality checks.

**Rule of thumb:** if stdlib has a *non-predicate* helper (`Sort`, `Clone`, `Concat`, `Contains`, `Reverse`), use stdlib. For transforms (Map, Filter, GroupBy, KeyBy, Reduce, Chunk, set ops) and predicate search (ContainsBy, Find), use `lo`. Never hand-roll what either provides.

## Gotchas & Anti-Patterns

These are the mistakes that actually bite — especially coming from Python/JS, where the conventions differ.

### Callback argument order is not consistent across `lo`

There's no single rule, and the inconsistency is the trap:

| Function family | Callback signature |
|---|---|
| Slice transforms (`Map`, `Filter`, `FilterMap`, `FlatMap`, …) | `func(item T, index int)` |
| `MapValues` / `MapKeys` | `func(value V, key K)` — **value first** |
| `PickBy` / `OmitBy` / `MapToSlice` / `MapEntries` / `FilterKeys` / `FilterValues` | `func(key K, value V)` — **key first** |

A Python/JS dev reaches for `(key, value)` everywhere and silently swaps the args in `MapValues`. When both `K` and `V` are the same type (e.g. `map[string]string`) it still compiles and produces garbage. **Defense:** in the *map* functions, name **both** params (`func(val, key string)`, not `func(v string, _ string)`) so a swap reads wrong at a glance — the linter here doesn't flag a named-but-unused closure param, so there's no cost. (Slice-transform callbacks keep `_ int` for the index — it's positionally unambiguous; only key-vs-value is the trap.) When unsure, copy the order from the per-function example in `references/map-transforms.md` rather than guessing.

### Parallel work that can fail → `errgroup`, not `lop`

`lo/parallel` callbacks return a single value with no `error` slot, and there are no `*Err` parallel variants. Since parallelism is almost always for I/O (which fails), `lop` rarely fits real work — use `golang.org/x/sync/errgroup` (`SetLimit(N)` to bound concurrency, first error cancels the rest). Full pattern in `references/concurrency.md`.

### Don't chain `Filter` then `Map` — fold them into `FilterMap`

```go
// Two passes, two allocations:
names := lo.Map(lo.Filter(users, func(u User, _ int) bool { return u.Active }),
    func(u User, _ int) string { return u.Name })

// One pass:
names := lo.FilterMap(users, func(u User, _ int) (string, bool) {
    return u.Name, u.Active
})
```
The inline form above is semgrep-enforced here (`no-lo-filter-then-map`) — with no intermediate variable, the fold is always safe. `FilterMap` is heavily used in this codebase (~32 call sites).

**But fold only when the filtered slice isn't reused.** When you keep the intermediate around for a length check, indexing, slicing, or sorting *before* the map, the two passes are doing real work and `FilterMap` can't express it — leave them separate:

```go
matches := lo.Filter(cols, func(c Collection, _ int) bool { return c.Name == input })
switch len(matches) {           // ← intermediate used for branching + indexing
case 0:
    return errNotFound
case 1:
    return matches[0], nil
default:
    keys := lo.Map(matches, func(c Collection, _ int) string { return c.Key }) // correct as-is
    ...
}
```
This is why the semgrep rule deliberately matches only the inline `lo.Map(lo.Filter(...))` form, not the two-statement version — the latter is a human judgment call, not a mechanical rewrite.

### `lo` is for *transforms*, not iteration-for-effect

If you're not building a new value, don't wrap a loop in `lo`. A plain `for range` for side effects is idiomatic Go and clearer than `lo.ForEach` for most cases — reach for `lo.ForEach` only when it reads better in a pipeline. Never use `lo.Map` and throw away the result.

### Prefer the named aggregator over a hand-rolled `Reduce`

`lo.SumBy`, `MaxBy`, `MinBy`, `CountBy`, `GroupBy` say what they mean; a `Reduce` that reimplements them makes the reader decode the accumulator. Reach for `Reduce` only when no named aggregator fits.

## Performance Notes

- `lo.Map` is ~4% slower than a hand-written `for` loop — negligible for all practical purposes.
- `lo.Map` is ~7x faster than reflection-based libraries (e.g., go-funk).
- `lop.Map` (parallel) adds goroutine overhead — only use for I/O-bound callbacks, not CPU-bound transforms.
- All `lo` functions are allocation-friendly — same profile as hand-written equivalents.

## Replacing Go Boilerplate

The whole point of `lo` here is deleting verbose loops so the *intent* survives instead of the mechanics — the win a Python/JS dev expects from a comprehension. When you're about to write the left column, write the right column instead. These are the exact rewrites `.semgrep/go-modern.yml` enforces (`just lint-style`), so the manual forms fail the gate anyway.

| Instead of this Go loop | Write |
|---|---|
| `out := make([]R, len(xs)); for i, x := range xs { out[i] = f(x) }` | `lo.Map(xs, func(x T, _ int) R { return f(x) })` |
| `for _, x := range xs { if keep(x) { out = append(out, x) } }` | `lo.Filter(xs, func(x T, _ int) bool { return keep(x) })` |
| `for _, x := range xs { if keep(x) { out = append(out, f(x)) } }` | `lo.FilterMap(xs, func(x T, _ int) (R, bool) { return f(x), keep(x) })` |
| `for _, x := range xs { out = append(out, x.Sub...) }` | `lo.FlatMap(xs, func(x T, _ int) []R { return x.Sub })` |
| `m := make(map[K]T); for _, x := range xs { m[x.K] = x }` | `lo.KeyBy(xs, func(x T) K { return x.K })` |
| `m := make(map[K]V); for _, x := range xs { m[x.K] = x.V }` | `lo.SliceToMap(xs, func(x T) (K, V) { return x.K, x.V })` |
| `m := make(map[K][]T); for _, x := range xs { m[x.K] = append(m[x.K], x) }` | `lo.GroupBy(xs, func(x T) K { return x.K })` |
| `n := 0; for _, x := range xs { if keep(x) { n++ } }` | `lo.CountBy(xs, func(x T) bool { return keep(x) })` |
| `for _, x := range xs { if x == target { return true } }; return false` | `slices.Contains(xs, target)` |
| `for _, x := range xs { if pred(x) { return true } }; return false` | `lo.ContainsBy(xs, pred)` |
| `for _, x := range xs { if pred(x) { return x, true } }` | `lo.Find(xs, pred)` |
| `for i, x := range xs { if pred(x) { return i, true } }` | `_, i, ok := lo.FindIndexOf(xs, pred)` |
| `seen := map[K]bool{}; for _, x := range xs { if !seen[x.K] { seen[x.K]=true; out=append(out,x) } }` | `lo.UniqBy(xs, func(x T) K { return x.K })` |
| `keys := []K{}; for k := range m { keys = append(keys, k) }` | `slices.Sorted(maps.Keys(m))` |
| `for k, v := range src { dst[k] = v }` | `maps.Copy(dst, src)` |
| `sort.Slice(xs, …)` / `sort.Strings(xs)` | `slices.SortFunc(xs, …)` / `slices.Sort(xs)` |

If the loop also branches on the intermediate (length checks, indexing, sorting), keep it explicit — see [Gotchas](#gotchas--anti-patterns) for when *not* to fold.

## Reference Files

- `references/slice-transforms.md` — Complete slice function catalog with signatures and examples
- `references/map-transforms.md` — Complete map function catalog
- `references/set-ops.md` — Set operations, search, and membership helpers
- `references/error-handling.md` — All `*Err` variants and error utilities
- `references/python-js-rosetta.md` — Side-by-side Python/JS → Go+lo translations
- `references/concurrency.md` — Parallel variants, channels, async, retry, debounce
- `references/misc.md` — Tuples, math, strings, time, pointer helpers, conditionals
