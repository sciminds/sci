# `slices`, `maps`, `cmp` — the collection toolbox

The three packages that replaced most hand-written collection code and the
legacy `sort` package. Everything here is stable stdlib. Version tags tell you
the minimum `go` directive needed.

## `slices` — base functions (Go 1.21, `Concat` 1.22)

### Sorting (replaces the `sort` package entirely)

| Function | Notes |
|---|---|
| `slices.Sort(s)` | ascending, for `cmp.Ordered` types |
| `slices.SortFunc(s, cmp)` | comparator returns `int` (`-1/0/+1`) |
| `slices.SortStableFunc(s, cmp)` | stable variant — preserves input order of equal elements |
| `slices.IsSorted(s)` / `IsSortedFunc(s, cmp)` | sortedness check |
| `slices.BinarySearch(s, target)` | `(index, found)` on a sorted slice |
| `slices.BinarySearchFunc(s, target, cmp)` | binary search with comparator |

```go
// The comparator returns an int, not a bool. Pair with cmp.Compare.
slices.SortFunc(users, func(a, b User) int { return cmp.Compare(a.Age, b.Age) })

// Descending: swap the operands.
slices.SortFunc(findings, func(a, b Finding) int { return cmp.Compare(b.Severity, a.Severity) })

// Stable, when ties must keep input order:
slices.SortStableFunc(rows, func(a, b Row) int { return cmp.Compare(a.Key, b.Key) })
```

`sort.Slice` / `sort.Strings` / `sort.Ints` / `sort.Sort` are **banned** here
(lint-guard rule 9). There is no case where the old `sort` package is the right
answer in new code.

### Search & compare

| Function | Returns / does |
|---|---|
| `slices.Contains(s, v)` | bool — membership by `==` |
| `slices.ContainsFunc(s, pred)` | bool — *but this project prefers `lo.ContainsBy`* |
| `slices.Index(s, v)` | first index of `v`, or `-1` |
| `slices.IndexFunc(s, pred)` | *prefer `lo.FindIndexOf`* |
| `slices.Equal(s1, s2)` | element-wise equality — replaces `reflect.DeepEqual` for slices |
| `slices.EqualFunc(s1, s2, eq)` | equality with a custom comparator |
| `slices.Compare(s1, s2)` | lexicographic `-1/0/+1` |
| `slices.Max(s)` / `slices.Min(s)` | extremes of a slice (vs. the `max`/`min` builtins on args) |
| `slices.MaxFunc(s, cmp)` / `slices.MinFunc(s, cmp)` | extremes with comparator |

> **Predicate split:** for `*Func` search (`ContainsFunc`, `IndexFunc`,
> `MaxFunc`, `MinFunc`) this project deliberately uses the `lo` equivalents
> (`ContainsBy`, `FindIndexOf`, `MaxBy`, `MinBy`) because they return the matched
> *value*, not just a bool/index, and keep the call style consistent with the
> rest of a pipeline. See the `lo` skill. Use the **non-`Func`** `slices`
> functions (`Contains`, `Index`, `Equal`, `Compare`) — those are plain-equality
> and belong here.

### Copy, join, edit

| Function | Replaces |
|---|---|
| `slices.Clone(s)` | `append([]T(nil), s...)` / `make+copy` (banned manually) |
| `slices.Concat(s1, s2, …)` (1.22) | nested `append(append(a, b...), c...)` |
| `slices.Compact(s)` / `CompactFunc(s, eq)` | manual adjacent-dedupe |
| `slices.Insert(s, i, v…)` | manual splice |
| `slices.Delete(s, i, j)` / `DeleteFunc(s, pred)` | manual splice |
| `slices.Replace(s, i, j, v…)` | manual splice |
| `slices.Reverse(s)` | manual swap loop |
| `slices.Repeat(s, count)` (1.23) | manual tiling |

```go
dup := slices.Clone(original)        // shallow copy
all := slices.Concat(a, b, c)        // one allocation, any number of slices

// Delete/Compact/Replace MUTATE the backing array and zero the freed tail —
// reassign the result, never ignore it:
s = slices.Delete(s, i, i+1)
s = slices.Compact(s)                // only removes ADJACENT dups — sort first for full dedupe
```

### Iterator functions (Go 1.23) — see `iterators.md`

| Function | Signature | Use |
|---|---|---|
| `slices.All(s)` | `iter.Seq2[int, E]` | index+value as an iterator |
| `slices.Values(s)` | `iter.Seq[E]` | values only |
| `slices.Backward(s)` | `iter.Seq2[int, E]` | reverse iteration |
| `slices.Collect(seq)` | `[]E` | drain an `iter.Seq` into a slice |
| `slices.AppendSeq(s, seq)` | `[]E` | append an iterator onto `s` |
| `slices.Sorted(seq)` | `[]E` | collect + sort |
| `slices.SortedFunc(seq, cmp)` / `SortedStableFunc` | `[]E` | collect + sort with comparator |
| `slices.Chunk(s, n)` | `iter.Seq[[]E]` | batch into sub-slices of ≤ n (each shares backing array — copy if retained) |

## `maps` (Go 1.21 base, iterators 1.23)

**Go 1.21 (non-iterator):**

| Function | Does |
|---|---|
| `maps.Clone(m)` | shallow copy of a map |
| `maps.Copy(dst, src)` | merge `src` into `dst` (src wins) — replaces a for-range assign loop |
| `maps.Equal(m1, m2)` / `EqualFunc` | map equality |
| `maps.DeleteFunc(m, pred)` | delete entries matching a predicate, in place |

**Go 1.23 (iterators — note these return `iter.Seq`, NOT slices):**

| Function | Signature |
|---|---|
| `maps.Keys(m)` | `iter.Seq[K]` |
| `maps.Values(m)` | `iter.Seq[V]` |
| `maps.All(m)` | `iter.Seq2[K, V]` |
| `maps.Collect(seq)` | `map[K]V` — build a map from an `iter.Seq2` |
| `maps.Insert(m, seq)` | insert key/value pairs from an `iter.Seq2` |

```go
// Canonical deterministic-output idiom (this codebase uses it everywhere):
for _, k := range slices.Sorted(maps.Keys(m)) { fmt.Println(k, m[k]) }

// Just want a []K (order undefined)?
keys := slices.Collect(maps.Keys(m))

// Merge / copy:
maps.Copy(dst, src)                  // not: for k, v := range src { dst[k] = v }
clone := maps.Clone(config)          // shallow

// Sum the values:
total := lo.Sum(slices.Collect(maps.Values(counts)))
```

> **Migration trap:** the old `golang.org/x/exp/maps.Keys(m)` returned `[]K`. The
> stdlib version returns `iter.Seq[K]`. Code that did `ks := maps.Keys(m); sort.Strings(ks)`
> becomes `ks := slices.Sorted(maps.Keys(m))`. Drop the `x/exp` import.

## `cmp` (Go 1.21, `Or` 1.22)

| Function | Does |
|---|---|
| `cmp.Compare[T](a, b)` | `-1/0/+1` for ordered types — the comparator for `SortFunc` |
| `cmp.Less[T](a, b)` | bool (rarely needed; `Compare` covers sorting) |
| `cmp.Or[T](vals…)` (1.22) | first non-zero value — null-coalescing / default precedence |
| `cmp.Ordered` | the constraint for "things `<` works on" |

```go
// Defaults / precedence — first non-empty wins:
kind := cmp.Or(flagKind, envKind, "python")
spec := cmp.Or(entry.Spec, entry.Name)

// Multi-key sort — first differing field decides:
slices.SortFunc(people, func(a, b Person) int {
    return cmp.Or(
        cmp.Compare(a.Last, b.Last),
        cmp.Compare(a.First, b.First),
    )
})
```

**Gotcha:** `cmp.Or` evaluates *all* arguments (no short-circuit) — they're
ordinary function arguments. Don't put a side-effecting or expensive call in it
expecting later args to be skipped.

## Builtins vs. `slices.Max`/`Min`

- `max(a, b, c)` / `min(a, b, c)` — builtins, operate on a **fixed list of
  arguments** of any ordered type. Use for clamps and pairwise comparison.
- `slices.Max(s)` / `slices.Min(s)` — operate on a **slice**. Panic on empty
  input, so guard length first.

```go
w := max(min(width, hardCap), floor)   // builtin — clamp
hi := slices.Max(scores)               // slice — guard len(scores) > 0 first
```
