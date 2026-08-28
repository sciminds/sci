---
name: go-modern
description: >-
  Write modern, idiomatic Go (1.21–1.27) using the standard library and current
  language features instead of legacy patterns. Covers slices/maps/cmp, the
  min/max/clear builtins, range-over-int and range-over-func iterators,
  errors.Is/As/Join and %w wrapping, generics (including 1.27 generic methods
  and expanded type inference), embedded-field struct-literal keys, new(expr),
  omitzero, the now-stable encoding/json/v2, the stdlib uuid package, os.Root,
  slog, and the stdlib APIs that are now the recommended idiom. Use this skill
  BEFORE writing or editing any Go (.go) code, reviewing a Go diff, or picking a
  stdlib API — it replaces the legacy forms this project's lint rules ban (the
  old sort package, interface{}, manual index loops, append([]T(nil), …),
  ptr() helpers, hand-rolled min/max). Complements the `lo` skill, which owns
  functional transforms (Map/Filter/Reduce/GroupBy); reach here for everything
  stdlib- and language-level. Invoke whenever the task is "write some Go",
  "clean this Go up", "is there a stdlib way to do this", or CLAUDE.md's
  Skills table routes a Go edit here.
version: 1.1.0
license: MIT
---

# Modern Go — stdlib & language idioms (Go 1.21–1.27)

Go has changed a lot since generics landed. Whole categories of boilerplate that
defined "Go code" five years ago — index loops, the `sort.Slice` dance, `x := x`
closure copies, `interface{}`, hand-rolled `min`/`max`, `append([]T(nil), s...)`
to clone — now have a one-line stdlib or language answer. This skill is the
reference for writing Go the way the current toolchain wants it written:
**clearer, shorter, and harder to get wrong.**

The audience for this codebase is Python/JS developers learning Go. The modern
idioms are a gift to them — they read closer to what they already know. Prefer
them not because they're new, but because they say what they mean.

## Relationship to the `lo` skill — read this first

These two skills split the work cleanly. Don't duplicate; hand off.

| You're reaching for… | Skill |
|---|---|
| `Map`, `Filter`, `Reduce`, `GroupBy`, `KeyBy`, `FilterMap`, set ops (Intersect/Difference/Union), `ContainsBy`/`FindIndexOf` predicate search | **`lo`** skill |
| `slices.*`, `maps.*`, `cmp.*`, `min`/`max`/`clear`, iterators, `errors.*`, generics, `new(expr)`, `omitzero`, `os.Root`, `slog`, `sync`, `testing` | **this skill** |

The boundary is **transforms vs. stdlib/language**. If you're building a new
collection by transforming another (`lo.Map`) or grouping (`lo.GroupBy`), that's
`lo`. If you're sorting, cloning, searching by equality, extracting keys,
wrapping errors, or using a language feature, that's here. When both could apply
(e.g. `slices.ContainsFunc` vs `lo.ContainsBy`), **this project prefers the `lo`
predicate forms** — see the `lo` skill's "stdlib vs lo" table for why. Reserve
the plain `slices.Contains` (equality, no `Func`) for this skill's territory.

Go 1.27 doesn't move this boundary: generic methods and the wider inference add
no stdlib transform API, so Map/Filter/GroupBy/set-ops still route to `lo`
(which itself stays on a go 1.18 baseline and hasn't adopted generic methods).

## When to use this skill

- Writing or editing any `.go` file in this repo
- Choosing a standard-library API ("is there a built-in for this?")
- Reviewing a Go diff for legacy patterns
- Replacing a manual loop that *isn't* a transform (sorting, key extraction, map merge, membership)
- Anything `just lint-guard` / `just lint-style` rejects — CLAUDE.md's "Lint enforcement" section lists the rules and this skill is what keeps them quiet

## The modern-Go reflexes (legacy → modern lookup)

The highest-value swaps, sorted by how often they come up. Each has detail below
or in a reference file. If you internalize one table, make it this one.

| Instead of (legacy) | Write (modern) | Since |
|---|---|---|
| `sort.Slice(s, func(i,j) bool {...})` | `slices.SortFunc(s, func(a,b T) int { return cmp.Compare(...) })` | 1.21 |
| `math.Max(float64(a), float64(b))` / helper funcs | `max(a, b)` / `min(a, b)` (any ordered type) | 1.21 |
| `append([]T(nil), s...)` / `make+copy` to clone | `slices.Clone(s)` | 1.21 |
| `append(append(a, b...), c...)` | `slices.Concat(a, b, c)` | 1.22 |
| linear loop to test membership | `slices.Contains(s, v)` | 1.21 |
| `keys := []K{}; for k := range m {...}; sort` | `slices.Sorted(maps.Keys(m))` | 1.23 |
| `for k, v := range src { dst[k] = v }` | `maps.Copy(dst, src)` | 1.21 |
| `if a != "" { x=a } else if b != "" {...}` chains | `cmp.Or(a, b, "default")` | 1.22 |
| `for i := 0; i < n; i++ {}` | `for i := range n {}` (or `for range n {}`) | 1.22 |
| `v := v` inside a loop before a goroutine/closure | *delete it* — each iteration is fresh | 1.22 |
| `func ptr[T any](v T) *T { return &v }` then `ptr(x)` | `new(x)` | 1.26 |
| `interface{}` | `any` | 1.18 |
| `for _, line := range strings.Split(s, "\n")` | `for line := range strings.Lines(s)` | 1.24 |
| `var perr *PathError; errors.As(err, &perr)` | `perr, ok := errors.AsType[*PathError](err)` | 1.26 |
| `time.Time` field with `json:"…,omitempty"` (never omits!) | `json:"…,omitzero"` | 1.24 |
| `runtime.SetFinalizer(obj, …)` | `runtime.AddCleanup(obj, …)` | 1.24 |
| `rand.Seed(…); rand.Intn(n)` | `rand.IntN(n)` (math/rand/v2, auto-seeded) | 1.22 |
| package-level generic helper that belongs to one type | a generic *method* on that type | 1.27 |
| `Outer{Embedded: Embedded{Field: v}}` just to set one field | `Outer{Field: v}` — embedded/nested fields are literal keys now | 1.27 |
| explicit type args in literals/conversions/sends: `[]Fn{Generic[int]}` | drop them — inference now covers all assignment contexts | 1.27 |
| `github.com/google/uuid` dependency | stdlib `uuid` package (`uuid.New()`, `uuid.Parse`) | 1.27 |
| `i := strings.LastIndex(s, sep)` + manual slicing | `before, after, ok := strings.CutLast(s, sep)` | 1.27 |
| hand-rolled deep copy of `url.URL` / `url.Values` | `u.Clone()` / `vals.Clone()` | 1.27 |

A mechanical first pass: **`go fix` applies many of these automatically** — see
`references/migration-checklist.md`. But knowing them
up front means you write modern Go the first time instead of relying on a linter
to catch you, which is the whole point of this skill.

## Language features

Brief tour; full detail and gotchas in `references/language.md`.

**`min` / `max` / `clear` builtins (1.21)** — no import, any ordered type.
```go
w := max(min(width-4, descMax), 20)   // clamp — reads exactly as intended
clear(cache)                          // empty a map; on a slice, zeroes elements (keeps len)
```

**Per-iteration loop variables (1.22)** — the loop variable is fresh each
iteration, so the old `x := x` shadow before a goroutine or closure is dead
code. Delete it.
```go
for _, job := range jobs {
    go func() { run(job) }()   // each goroutine captures its own job — no copy needed
}
```

**Range over integers (1.22)** — `for i := range n` counts `0..n-1`; drop the
index entirely with `for range n` when you just want "do this n times."

**`any` over `interface{}` (1.18)** — pure readability; `any` is an exact alias.
`gofmt` won't rewrite it, so write `any` from the start.

**`new(expr)` (1.26)** — `new` now takes an expression and returns a pointer to a
fresh variable holding that value. This kills the `ptr()`/`addr()` helper that
every Go codebase grows for optional/nullable struct and JSON fields.
```go
cfg := Config{Timeout: new(30 * time.Second)}   // *time.Duration, no helper
```

**Generic type aliases (1.24)** — aliases can take type parameters now, so you
can shorten a verbose instantiation without minting a new defined type:
```go
type Set[T comparable] = map[T]struct{}   // a true alias, identical to the underlying type
```

**Generic methods (1.27)** — methods can now declare their own type parameters.
This kills two long-standing workarounds: the per-type method families
(`IntN`/`Int32N`/`Int64N`…) and the package-level generic function that really
belongs to a type (`ProcessCache[T](c *Cache, ...)`). If a generic helper takes
your type as its first argument, it probably wants to be a method now.
```go
// Before: one method per type, or a free function polluting the package namespace.
func (c *Cache) GetString(key string) (string, bool)
func (c *Cache) GetInt(key string) (int, bool)

// 1.27: one generic method.
func (c *Cache) Get[T any](key string) (T, bool)
```
Limit: interface methods can't have type parameters, and a generic method can't
satisfy an interface — this is for concrete types.

**Embedded/nested fields as struct-literal keys (1.27)** — a literal key can be
any valid field selector, so promoted fields initialize directly instead of
forcing you to spell out the intermediate struct:
```go
type Gopher struct {
    Name string
    Habitat        // embedded
}

g := Gopher{Name: "Gopher", Burrow: "Burrow #42"}
// was: Gopher{Name: "Gopher", Habitat: Habitat{Burrow: "Burrow #42"}}
```
This makes embedding much cheaper to use — options/config structs composed via
embedding no longer leak their internal layering into every construction site.

**Type inference in all assignment contexts (1.27)** — generic functions no
longer need explicit type arguments in composite literals, conversions, or
channel sends; anywhere the target function type is known, `T` is inferred:
```go
type IntFormatter func(int) string
formatters := []IntFormatter{GenericFormatter}   // was GenericFormatter[int]
fn := IntFormatter(GenericFormatter)
ch <- GenericFormatter
```

## Standard library: the collection toolbox (`slices`, `maps`, `cmp`)

This is the bread and butter — the most-used modern stdlib in this repo. Full
catalog with every function and signature in `references/slices-maps-cmp.md`.

**Sorting** — `slices.SortFunc` takes a comparator returning an `int`
(`-1/0/+1`), **not** a `bool` `less`. Pair it with `cmp.Compare`:
```go
slices.SortFunc(users, func(a, b User) int { return cmp.Compare(a.Name, b.Name) })
```
For multi-key sorts, chain tie-breakers with `cmp.Or` — it returns its first
non-zero argument, so the first differing field decides:
```go
slices.SortFunc(people, func(a, b Person) int {
    return cmp.Or(
        cmp.Compare(a.Last, b.Last),
        cmp.Compare(a.First, b.First),
        cmp.Compare(a.Age, b.Age),
    )
})
```

**`cmp.Or` for defaults (1.22)** — Go's null-coalescing. First non-zero value
wins; great for config precedence:
```go
kind := cmp.Or(flagKind, envKind, "python")
```

**Deterministic map keys** — `maps.Keys` returns an *iterator* (1.23), not a
slice. The canonical "sorted unique keys" pipeline:
```go
for _, k := range slices.Sorted(maps.Keys(m)) { ... }   // sorted, deterministic output
keys := slices.Collect(maps.Keys(m))                    // just want a []K, unsorted
```

**Clone / concat / merge** — never hand-roll these (the linter bans the manual
forms):
```go
dup  := slices.Clone(original)   // not append([]T(nil), original...)
all  := slices.Concat(a, b, c)   // not nested appends
maps.Copy(dst, src)              // not a for-range assignment loop
b2   := bytes.Clone(b)           // not make([]byte, len(b)) + copy
```

**Membership** — `slices.Contains(s, v)` for plain equality. For *predicate*
search, this project routes to `lo.ContainsBy` / `lo.Find` / `lo.FindIndexOf`
(see the `lo` skill), not `slices.ContainsFunc` / `IndexFunc`.

## Iterators — range-over-func (Go 1.23+)

`for range` now accepts iterator functions: `iter.Seq[V]` (`func(yield func(V) bool)`)
and `iter.Seq2[K, V]`. This is what powers `maps.Keys`, `slices.Values`,
`strings.Lines`, etc. Full guide — consuming, *writing* your own, and when *not*
to bother — in `references/iterators.md`.

The everyday win is the lazy string/bytes splitters (1.24), which stream instead
of allocating a whole `[]string`:
```go
for line := range strings.Lines(text) { ... }   // not strings.Split(text, "\n")
for field := range strings.FieldsSeq(s) { ... }
```
The codebase consumes iterators (`slices.Sorted(maps.Keys(...))`) constantly but
hasn't needed to *write* custom ones yet. When you build a lazy/streaming API,
reach for `iter.Seq`; when a plain slice is clearer and small, return the slice.

## Errors

Full patterns in `references/errors.md`. The essentials:

**Wrap with `%w` to preserve the chain** (use exactly one `%w` per `Errorf`):
```go
return fmt.Errorf("load config %q: %w", path, err)
```
**`errors.Is` for sentinels, `errors.As`/`AsType` for typed errors:**
```go
if errors.Is(err, fs.ErrNotExist) { ... }                 // identity through the chain
if perr, ok := errors.AsType[*fs.PathError](err); ok {     // 1.26 — typed, returns the value
    log.Print(perr.Path)
}
```
**`errors.Join` to accumulate multiple failures** (nil-safe; skips nils):
```go
var errs error
for _, f := range files {
    errs = errors.Join(errs, process(f))
}
return errs   // nil if every call succeeded
```

## Other modern stdlib worth knowing

One-liners; depth and before/after in `references/stdlib-extras.md`.

- **`slog` (1.21)** — structured logging in the stdlib. *Note: this codebase
  deliberately doesn't log — CLI output flows through `cmdutil.Result` (JSON +
  human). Reach for `slog` only if that changes, e.g. a long-running daemon.*
- **`omitzero` json tag (1.24)** — fixes `omitempty`'s failure on `time.Time`
  and other structs (a zero struct is never "empty"). Use `omitzero` for
  time/struct/custom fields; `omitempty` stays fine for scalars/slices/maps.
- **`os.Root` (1.24, expanded 1.25)** — sandbox filesystem access to a directory;
  resists `../` traversal and symlink escape. Use for any untrusted relative path.
- **`math/rand/v2` (1.22)** — auto-seeded, better algorithms, generic `rand.N`.
  `Intn → IntN`. (Crypto still uses `crypto/rand`.)
- **`unique` (1.23)** — intern repeated comparable values to cut memory and make
  equality a pointer compare.
- **`sync.WaitGroup.Go` (1.25)** — bundles `Add(1)` + `go` + `defer Done()`:
  `wg.Go(func() { run(job) })`. For bounded concurrency or error collection, still
  use `errgroup` (see the `lo` skill's concurrency reference).
- **`testing/synctest` (stable 1.25)** — `synctest.Test(t, ...)` virtualizes time
  so timeout/ticker tests run instantly and deterministically.
- **`testing.B.Loop` (1.24)** — `for b.Loop() {}` instead of `for i := 0; i < b.N; i++ {}`;
  setup before the loop runs once.
- **Container-aware `GOMAXPROCS` (1.25)** — the runtime respects cgroup CPU limits
  automatically; you can usually drop `go.uber.org/automaxprocs`.
- **`encoding/json/v2` (stable 1.27)** — configurable options, stricter defaults,
  much faster unmarshal. v1 `encoding/json` is now backed by it, so existing code
  speeds up for free; new code can adopt the v2 API. Keep using `omitzero`.
- **`uuid` (1.27)** — UUIDs in the stdlib: `uuid.New()` (v4), `uuid.NewV7()`
  (time-ordered — prefer for DB keys), `uuid.Parse`/`MustParse`. Drop the
  `github.com/google/uuid` dependency in new code.
- **`strings.CutLast` / `bytes.CutLast` (1.27)** — split around the *last*
  separator: `dir, file, ok := strings.CutLast(path, "/")`. Replaces the
  `LastIndex` + slicing dance, same shape as `strings.Cut`.
- **`httptest.NewTestServer` (1.27)** — `httptest.NewTestServer(t, handler)`
  runs an HTTP test server on an in-memory fake network, auto-cleans up via
  `t.Cleanup`, and composes with `testing/synctest` for instant timeout tests.
- **`goroutineleak` pprof profile (stable 1.27)** — the runtime detects
  permanently blocked goroutines via GC reachability; grab
  `/debug/pprof/goroutineleak` or `pprof.Lookup("goroutineleak")` when hunting leaks.

## This project's guardrails

The point of this skill is to write modern Go *ahead of time* so the linters stay
quiet. What they enforce (so you don't have to learn it by getting flagged):

- **The old `sort` package is banned** (lint-guard rule 9) — use `slices.Sort` /
  `slices.SortFunc` / `slices.SortStableFunc` / `slices.BinarySearch`.
- **Manual clone is banned** — `slices.Clone` (rule 10), `bytes.Clone` (rule 11),
  not `append([]T(nil), …)` or `make+copy`.
- **`any`, never `interface{}`** in hand-written code.
- **Manual transform loops → `lo`** — a whole family of semgrep rules
  (`.semgrep/go-modern.yml`) rewrites `for`+`append` into `lo.Map`/`Filter`/etc.
  That's the `lo` skill's job; see it for the catalog.

Self-check before you call a change done:
```
just lint-style    # semgrep + ast-grep (the go-modern.yml rules)
just lint-guard    # the structural rules above (sort/clone/any bans)
just ok            # full gate: fmt + vet + lint + test + build
```
A mechanical sweep of an existing file: `go fix` (1.26+) applies most of the
legacy→modern rewrites. See `references/migration-checklist.md`.

## Gotchas

The traps that actually bite — most are "the modern API has a different shape
than the old one," which is exactly when a Python/JS dev guesses wrong.

- **`slices.SortFunc` wants `int`, not `bool`.** The comparator returns
  `-1/0/+1` (use `cmp.Compare`), unlike `sort.Slice`'s `less bool`. Returning a
  bool won't compile, but returning `0`/`1` by hand (forgetting the negative case)
  silently mis-sorts.
- **`maps.Keys` / `slices.Values` are iterators, not slices.** Migrating off
  `golang.org/x/exp/maps`? The stdlib `maps.Keys(m)` returns `iter.Seq[K]` —
  wrap with `slices.Sorted(...)` or `slices.Collect(...)` to get a `[]K`.
- **`slices.Compact` only removes *adjacent* duplicates.** Sort first for a full
  dedupe (or use `lo.Uniq`).
- **`slices.Delete` / `Compact` / `Replace` mutate the backing array** and zero
  the tail — always reassign the result: `s = slices.Delete(s, i, j)`.
- **`clear(slice)` zeroes elements but keeps `len`.** It is *not* `s = s[:0]`.
- **`strings.Lines` keeps the trailing `\n`** on each line (unlike
  `strings.Split(s, "\n")`). `TrimSuffix` if you don't want it.
- **Writing an iterator: honor `yield`'s bool return.** If `yield` returns
  `false` (consumer did `break`), stop — otherwise the consumer's `break` leaks.
- **`cmp.Or` evaluates *all* its arguments** (they're ordinary function args, no
  short-circuit). Don't put expensive or side-effecting calls inside it.
- **`new(expr)` vs `&` (1.26):** `new(f())` makes a fresh variable initialized to
  `f()`; for a composite literal you already have, `&T{...}` is still the idiom.
  Use `new(expr)` to replace `ptr(scalarExpr)` helpers, not every `&`.
- **Generic methods can't satisfy interfaces (1.27).** Interface method sets
  can't declare type parameters, and a generic method never implements an
  interface method. Design the interface around concrete instantiations instead.
- **1.27 language features need `go 1.27` in go.mod.** Generic methods,
  embedded-field literal keys, and the wider inference only activate when the
  module's `go` directive says so — a lower directive keeps the old rules even
  on a 1.27 toolchain.
- **json/v2 defaults are stricter than v1.** The v2 API rejects invalid UTF-8
  and duplicate object keys instead of silently accepting them; v1 callers are
  unaffected (v1 semantics are preserved), but error message *text* may differ
  now that v1 is backed by v2 — don't assert on it in tests.

## Reference files

Load these for depth when the SKILL.md summary isn't enough:

- `references/slices-maps-cmp.md` — Full `slices` / `maps` / `cmp` catalog: every
  useful function, signature, version, and before/after.
- `references/language.md` — Loop vars, range-over-int, `min`/`max`/`clear`,
  generics, generic type aliases & generic methods, embedded-field literal keys,
  expanded type inference, `new(expr)`, self-referential constraints, `any`.
- `references/iterators.md` — Range-over-func, `iter.Seq`/`Seq2`, consuming and
  writing iterators, the `slices`/`maps`/`strings`/`bytes` iterator functions.
- `references/errors.md` — `%w` wrapping, `Is`/`As`/`AsType`, `Join`, sentinel vs
  typed errors, multi-error patterns.
- `references/stdlib-extras.md` — `slog`, json `omitzero` + stable json/v2,
  `uuid`, `strings.CutLast`, `os.Root`, `math/rand/v2`, `unique`,
  `sync.WaitGroup.Go`, `synctest` (+ `NewTestServer`), `b.Loop`, GOMAXPROCS,
  `runtime.AddCleanup`, `goroutineleak` profiles.
- `references/migration-checklist.md` — Legacy→modern mapping, `go fix`
  modernizers, this project's lint rules, and a self-review checklist.
