# Iterators — range-over-func (Go 1.23+)

Since Go 1.23, `for range` accepts iterator functions. This is the machinery
behind `maps.Keys`, `slices.Values`, `strings.Lines`, and friends. You'll
*consume* iterators constantly; you'll *write* them occasionally, for lazy or
streaming APIs.

## The two shapes

The `iter` package defines the canonical signatures:

```go
type Seq[V any]     func(yield func(V) bool)
type Seq2[K, V any] func(yield func(K, V) bool)
```

A third accepted form, `func(func() bool)`, yields nothing (a pure sequence of
"ticks") — rare.

## Consuming iterators

Just `range` them — they read like any other range:

```go
for k := range maps.Keys(m) { ... }          // iter.Seq[K]
for i, v := range slices.All(s) { ... }       // iter.Seq2[int, E]
for line := range strings.Lines(text) { ... } // iter.Seq[string]
```

Drain into a concrete collection when you need one:

```go
keys   := slices.Collect(maps.Keys(m))   // []K, order undefined
sorted := slices.Sorted(maps.Keys(m))    // []K, sorted
asMap  := maps.Collect(pairsSeq)         // map[K]V from an iter.Seq2
```

`break` / early `return` in the loop body cleanly stops a well-behaved iterator.

## The `slices` / `maps` iterator functions

| Producing an iterator | From |
|---|---|
| `slices.Values(s)` | slice → values |
| `slices.All(s)` | slice → (index, value) |
| `slices.Backward(s)` | slice → reverse (index, value) |
| `slices.Chunk(s, n)` | slice → sub-slices of ≤ n |
| `maps.Keys(m)` / `maps.Values(m)` | map → keys / values |
| `maps.All(m)` | map → (key, value) |

| Consuming an iterator | To |
|---|---|
| `slices.Collect(seq)` | `[]E` |
| `slices.Sorted(seq)` / `SortedFunc(seq, cmp)` | sorted `[]E` |
| `slices.AppendSeq(s, seq)` | append onto existing slice |
| `maps.Collect(seq2)` | `map[K]V` |
| `maps.Insert(m, seq2)` | insert pairs into `m` |

## Lazy string / bytes splitters (Go 1.24)

The biggest everyday win: stream lines/fields without allocating a whole
`[]string`. Same names exist in `bytes` for `[]byte`.

| Function | Yields |
|---|---|
| `strings.Lines(s)` | each line **including its trailing `\n`** |
| `strings.SplitSeq(s, sep)` | substrings split on `sep` |
| `strings.SplitAfterSeq(s, sep)` | substrings with `sep` retained |
| `strings.FieldsSeq(s)` | whitespace-delimited fields |
| `strings.FieldsFuncSeq(s, pred)` | fields split where `pred` is true |

```go
for line := range strings.Lines(text) {          // not strings.Split(text, "\n")
    process(strings.TrimSuffix(line, "\n"))       // Lines keeps the newline
}
for field := range strings.FieldsSeq(row) { ... } // not strings.Fields(row)
```

## Writing your own iterator

Return a closure of the right shape. The contract: call `yield` for each
element, and **stop the moment `yield` returns `false`** (the consumer did
`break` or returned early). Ignoring that return value leaks work and breaks
`break`.

```go
// iter.Seq — a lazy line reader that doesn't build a slice.
func Lines(s string) iter.Seq[string] {
    return func(yield func(string) bool) {
        for _, ln := range strings.Split(s, "\n") {
            if !yield(ln) {
                return // consumer stopped — respect it
            }
        }
    }
}

// iter.Seq2 — yield key/value pairs.
func Enumerate[T any](items []T) iter.Seq2[int, T] {
    return func(yield func(int, T) bool) {
        for i, v := range items {
            if !yield(i, v) {
                return
            }
        }
    }
}
```

For an iterator that holds a resource (open file, DB rows), `defer` the cleanup
*inside* the returned closure so it runs whether the consumer finishes or breaks:

```go
func rows(db *sql.DB, q string) iter.Seq[Row] {
    return func(yield func(Row) bool) {
        rs, err := db.Query(q)
        if err != nil { return }
        defer rs.Close()              // runs on normal end AND on early break
        for rs.Next() {
            var r Row
            _ = rs.Scan(&r)
            if !yield(r) { return }
        }
    }
}
```

## When NOT to use an iterator

Iterators shine for **lazy, large, or streaming** sequences. They cost a closure
and indirection. If the data is small and already in memory, returning a plain
`[]T` (or a `lo` transform) is clearer and faster. This codebase consumes
iterators heavily but hasn't needed to hand-write one yet — let a real
laziness/streaming requirement pull you toward writing one, not novelty.
