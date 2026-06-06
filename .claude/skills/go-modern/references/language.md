# Language features (Go 1.21–1.26)

Language-level changes that remove boilerplate. Each notes the minimum `go`
directive required (features only activate when `go.mod` declares that version or
higher).

## `min` / `max` / `clear` builtins (1.21)

No import. `min`/`max` take one or more arguments of any ordered type and return
the same type — no `math.Max` float-only casting.

```go
hi := max(a, b)
clamped := max(lo, min(v, hi))          // clamp idiom
longest := max(len(a), len(b), len(c))  // any number of args
```

`clear` empties a map or zeroes a slice:
```go
clear(cache)   // map: removes all entries
clear(buf)     // slice: sets every element to the zero value — len is UNCHANGED
```
Gotchas: `clear(slice)` is **not** `s = s[:0]` (length stays). With floats,
`max` propagates NaN and treats `-0.0 < +0.0` per the spec.

## Per-iteration loop variables (1.22)

Each iteration of a `for` loop gets a **fresh** copy of the loop variable. The
classic closure-capture bug — and the `x := x` shadow that worked around it — are
gone.

```go
// Before 1.22 (mandatory shadow, or all goroutines saw the last value):
for _, v := range items {
    v := v
    go func() { handle(v) }()
}

// 1.22+ — delete the shadow:
for _, v := range items {
    go func() { handle(v) }()   // each goroutine captures its own v
}
```

Notes:
- Active only when `go.mod` declares `go 1.22` or later (this repo is well past).
- `vet`'s `loopclosure` check is retired; leftover `x := x` lines are harmless
  no-ops — delete them (`go fix` does).
- Rare behavior change: code that *relied* on sharing one variable across
  iterations now sees per-iteration copies. Almost always that code was buggy.

## Range over integers (1.22)

```go
for i := range n { ... }   // i = 0 .. n-1
for range n { ... }        // "do this n times", no index variable
```
`n` must be an integer; a non-positive `n` iterates zero times. Replaces the
C-style `for i := 0; i < n; i++` for the common counting case.

## `any` over `interface{}` (1.18)

`any` is an exact alias for `interface{}` — same type, more readable. Use it
everywhere in hand-written code; this repo bans `interface{}` outside generated
files. `gofmt` does not auto-rewrite, so write `any` from the start (`go fix`
can rewrite an existing file).

## Generics — when to write your own

`samber/lo` covers the common transforms (see the `lo` skill), so you rarely need
to write generic functions. Reach for your own generic helper when:

- The logic isn't a transform `lo` already provides (e.g. a typed tree walk, a
  generic cache, a `Result[T]`-style wrapper).
- You'd otherwise copy-paste the same function for `int`, `string`, `float64`.

```go
// A small generic helper — constrain to exactly what you use.
func keysOf[K comparable, V any](m map[K]V) []K {
    return slices.Collect(maps.Keys(m))
}

// Constraints: cmp.Ordered for "< works"; comparable for "== works / map key";
// any for "no constraint". Define a custom interface constraint only when you
// need specific methods.
func clampOrdered[T cmp.Ordered](v, lo, hi T) T { return max(lo, min(v, hi)) }
```

Keep constraints as narrow as the body actually requires — `comparable` if you
only compare, `cmp.Ordered` if you order, a method interface if you call methods.

### Generic type aliases (1.24)

Type aliases can take type parameters. Use them to shorten a verbose
instantiation **without** creating a new defined type (an alias keeps the
underlying type's identity and method set; a defined type starts fresh).

```go
type Set[T comparable]        = map[T]struct{}     // alias — interchangeable with map[T]struct{}
type Result[T any]            = func() (T, error)
```

Contrast with a *defined* generic type, which you use when you want to attach
methods or prevent accidental interchange:
```go
type Stack[T any] []T          // defined type — can have methods, not an alias
func (s *Stack[T]) Push(v T) { *s = append(*s, v) }
```

## `new(expr)` (1.26)

`new` now accepts an expression and returns a pointer to a fresh variable
initialized to that value. This retires the `ptr[T any](v T) *T { return &v }`
helper that codebases grow for optional/nullable fields.

```go
// Before — the ubiquitous helper:
func ptr[T any](v T) *T { return &v }
cfg := Config{Retries: ptr(3), Timeout: ptr(30 * time.Second)}

// 1.26:
cfg := Config{Retries: new(3), Timeout: new(30 * time.Second)}
```

Use `new(expr)` to replace `ptr(scalarOrExpr)`. For a composite literal you
already have, `&T{...}` remains the idiom — `new(T{...})` works but reads no
better. Don't mechanically convert every `&x`.

## Self-referential generic constraints (1.26)

A generic type may now reference itself in its own type-parameter list — the
"curiously recurring" pattern, useful for fluent/builder APIs and algebraic
interfaces.

```go
type Adder[A Adder[A]] interface { Add(A) A }

func sum[A Adder[A]](xs ...A) (out A) {
    for _, x := range xs { out = out.Add(x) }
    return out
}
```
Niche — most code never needs it — but it unblocks designs that previously
required `any` plus runtime assertions.
