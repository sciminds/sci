# Slice Transforms — Complete Reference

All callbacks receive `(element, index)`. The `index` parameter can be ignored with `_`.

## Map

Transform every element. Output slice has same length as input.

```go
lo.Map([]int64{1, 2, 3}, func(x int64, _ int) string {
    return strconv.FormatInt(x, 10)
})
// []string{"1", "2", "3"}
```

**Err:** `lo.MapErr` — callback returns `(R, error)`, short-circuits on first error.
**Parallel:** `lop.Map` — runs callbacks in goroutines, preserves order. Use for I/O-bound work only.
**Mutable:** `lom.Map` — updates in place (same type only).

## UniqMap

Map + deduplicate results in one pass.

```go
lo.UniqMap(users, func(u User, _ int) string { return u.Department })
// []string{"Engineering", "Sales"} (no dupes)
```

## Filter

Keep elements where predicate returns true.

```go
lo.Filter([]int{1, 2, 3, 4}, func(x int, _ int) bool {
    return x%2 == 0
})
// []int{2, 4}
```

**Err:** `lo.FilterErr`
**Mutable:** `lom.Filter`

## Reject

Inverse of Filter — keep elements where predicate returns false.

```go
lo.Reject([]int{1, 2, 3, 4}, func(x int, _ int) bool {
    return x%2 == 0
})
// []int{1, 3}
```

**Err:** `lo.RejectErr`

## FilterReject

Split into matched and rejected in one pass.

```go
kept, rejected := lo.FilterReject([]int{1, 2, 3, 4}, func(x int, _ int) bool {
    return x%2 == 0
})
// kept: []int{2, 4}, rejected: []int{1, 3}
```

## FilterMap

Filter + transform in one pass. Callback returns `(result, include)`.

```go
lo.FilterMap([]string{"cpu", "gpu", "mouse"}, func(x string, _ int) (string, bool) {
    if strings.HasSuffix(x, "pu") {
        return strings.ToUpper(x), true
    }
    return "", false
})
// []string{"CPU", "GPU"}
```

## RejectMap

Opposite of FilterMap — keeps elements where the bool is false.

```go
lo.RejectMap([]int{1, 2, 3, 4}, func(x int, _ int) (int, bool) {
    return x * 10, x%2 == 0
})
// []int{10, 30}  (rejected the even ones, kept odd transformed)
```

## FlatMap

Transform + flatten. Callback returns a slice per element.

```go
lo.FlatMap([]int{1, 2, 3}, func(x int, _ int) []string {
    s := strconv.Itoa(x)
    return []string{s, s}
})
// []string{"1", "1", "2", "2", "3", "3"}
```

**Err:** `lo.FlatMapErr`

## Reduce / ReduceRight

Accumulate to a single value.

```go
sum := lo.Reduce([]int{1, 2, 3, 4}, func(agg int, item int, _ int) int {
    return agg + item
}, 0)
// 10
```

`lo.ReduceRight` iterates right-to-left.
**Err:** `lo.ReduceErr`, `lo.ReduceRightErr`

## ForEach / ForEachWhile

Side-effects over a slice.

```go
lo.ForEach(items, func(item Item, _ int) {
    fmt.Println(item.Name)
})

// Stop early:
lo.ForEachWhile(items, func(item Item, _ int) bool {
    if item.Done { return false }
    process(item)
    return true
})
```

**Parallel:** `lop.ForEach`

## GroupBy

Group elements by key. Returns `map[K][]V`.

```go
lo.GroupBy([]int{0, 1, 2, 3, 4, 5}, func(i int) int {
    return i % 3
})
// map[int][]int{0: {0, 3}, 1: {1, 4}, 2: {2, 5}}
```

**Err:** `lo.GroupByErr`
**Parallel:** `lop.GroupBy`

## GroupByMap

Group + transform simultaneously. Callback returns `(key, transformedValue)`.

```go
lo.GroupByMap(users, func(u User) (string, string) {
    return u.Department, u.Name
})
// map[string][]string{"Engineering": {"Alice", "Bob"}, ...}
```

**Err:** `lo.GroupByMapErr`

## PartitionBy

Like GroupBy but returns `[][]V` (ordered groups).

```go
lo.PartitionBy([]int{-2, -1, 0, 1, 2}, func(x int) string {
    if x < 0 { return "neg" }
    if x == 0 { return "zero" }
    return "pos"
})
// [][]int{{-2, -1}, {0}, {1, 2}}
```

**Err:** `lo.PartitionByErr`  |  **Parallel:** `lop.PartitionBy`

## KeyBy

Slice → map with unique keys. Callback extracts the key; element is the value.

```go
lo.KeyBy([]string{"a", "aa", "aaa"}, func(s string) int {
    return len(s)
})
// map[int]string{1: "a", 2: "aa", 3: "aaa"}
```

**Err:** `lo.KeyByErr`

## SliceToMap (alias: Associate)

Slice → map with custom key AND value. Callback returns `(key, value)`.

```go
lo.SliceToMap(users, func(u *User) (int, string) {
    return u.ID, u.Name
})
// map[int]string{1: "Alice", 2: "Bob"}
```

## FilterSliceToMap

Like SliceToMap but callback returns `(key, value, include)`.

```go
lo.FilterSliceToMap(users, func(u User) (int, string, bool) {
    return u.ID, u.Name, u.Active
})
```

## Keyify

Build a set (`map[T]struct{}`) from a slice. For membership testing.

```go
set := lo.Keyify([]int{1, 1, 2, 3})
// map[int]struct{}{1: {}, 2: {}, 3: {}}
_, exists := set[2]  // true
```

## Uniq / UniqBy

Deduplicate a slice.

```go
lo.Uniq([]int{1, 2, 2, 1})
// []int{1, 2}

lo.UniqBy([]int{0, 1, 2, 3, 4, 5}, func(i int) int { return i % 3 })
// []int{0, 1, 2}
```

**Err:** `lo.UniqByErr`

## Chunk

Split slice into groups of given size.

```go
lo.Chunk([]int{0, 1, 2, 3, 4}, 2)
// [][]int{{0, 1}, {2, 3}, {4}}
```

## Window / Sliding

Sliding windows.

```go
lo.Window([]int{1, 2, 3, 4, 5}, 3)
// [][]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}}

lo.Sliding([]int{1, 2, 3, 4, 5, 6}, 3, 2)  // size=3, step=2
// [][]int{{1, 2, 3}, {3, 4, 5}}
```

## Flatten / Concat

```go
lo.Flatten([][]int{{0, 1}, {2, 3}})
// []int{0, 1, 2, 3}

lo.Concat([]int{1, 2}, []int{3, 4})
// []int{1, 2, 3, 4}
```

Note: prefer `slices.Concat` from stdlib when available.

## Compact

Remove zero-value elements.

```go
lo.Compact([]string{"", "foo", "", "bar"})
// []string{"foo", "bar"}

lo.Compact([]int{0, 1, 0, 2})
// []int{1, 2}
```

## Interleave

Round-robin elements from multiple slices.

```go
lo.Interleave([]int{1, 4}, []int{2, 5}, []int{3, 6})
// []int{1, 2, 3, 4, 5, 6}
```

## Take / Drop

```go
lo.Take([]int{0, 1, 2, 3, 4}, 3)              // []int{0, 1, 2}
lo.TakeWhile(xs, func(v int) bool { return v < 3 })
lo.TakeFilter(xs, 3, func(v int, _ int) bool { return v%2 == 0 })  // first 3 even

lo.Drop([]int{0, 1, 2, 3, 4}, 2)              // []int{2, 3, 4}
lo.DropRight([]int{0, 1, 2, 3, 4}, 2)         // []int{0, 1, 2}
lo.DropWhile(xs, func(v string) bool { return len(v) <= 2 })
lo.DropByIndex(xs, 2, 4, -1)                  // drop by index (supports negative)
```

## Replace / Splice

```go
lo.Replace([]int{0, 1, 0, 1}, 0, 42, 1)       // []int{42, 1, 0, 1} (replace first N)
lo.ReplaceAll([]int{0, 1, 0, 1}, 0, 42)        // []int{42, 1, 42, 1}
lo.Splice([]string{"a", "b"}, 1, "x", "y")     // []string{"a", "x", "y", "b"}
```

## Subset / Slice

Safe slicing that never panics on out-of-bounds.

```go
lo.Subset([]int{0, 1, 2, 3, 4}, 2, 3)   // offset + length → []int{2, 3, 4}
lo.Slice([]int{0, 1, 2, 3, 4}, 1, 3)    // start:end → []int{1, 2}
```

## IsSorted / IsSortedBy

```go
lo.IsSorted([]int{0, 1, 2, 3})  // true
lo.IsSortedBy(items, func(item Item) int { return item.Priority })
```

## Fill / Repeat / RepeatBy

```go
lo.Fill([]int{0, 0, 0}, 42)           // []int{42, 42, 42}
lo.Repeat(3, "x")                     // []string{"x", "x", "x"}
lo.RepeatBy(5, func(i int) int { return i * i })  // []int{0, 1, 4, 9, 16}
```

**Err:** `lo.RepeatByErr`

## Times

Build slice via N invocations of a function.

```go
lo.Times(3, func(i int) string { return fmt.Sprintf("item-%d", i) })
// []string{"item-0", "item-1", "item-2"}
```

**Parallel:** `lop.Times`

## Count / CountBy / CountValues / CountValuesBy

```go
lo.Count([]int{1, 5, 1}, 1)                                          // 2
lo.CountBy([]int{1, 5, 1}, func(i int) bool { return i < 4 })        // 2
lo.CountValues([]string{"a", "b", "a"})                               // map[string]int{"a": 2, "b": 1}
lo.CountValuesBy(nums, func(v int) bool { return v%2 == 0 })         // map[bool]int{...}
```

**Err:** `lo.CountByErr`

## Sampling

```go
lo.Sample([]string{"a", "b", "c"})            // random element
lo.Samples([]string{"a", "b", "c"}, 2)        // 2 random unique elements
```

## Clone

Shallow copy.

```go
cloned := lo.Clone(original)
```

Note: prefer `slices.Clone` from stdlib.

## String-like Slice Operations

Analogues of `strings.Cut/CutPrefix/CutSuffix/Trim*`:

```go
left, right, ok := lo.Cut(slice, separator)
trimmed := lo.TrimPrefix(slice, prefix)
trimmed := lo.TrimSuffix(slice, suffix)
trimmed := lo.Trim(slice, cutset)
```
