# Map Transforms — Complete Reference

## Keys / Values

```go
lo.Keys(map[string]int{"foo": 1, "bar": 2})          // []string{"foo", "bar"}
lo.Values(map[string]int{"foo": 1, "bar": 2})        // []int{1, 2}
lo.UniqKeys(map1, map2)                               // deduplicated across maps
lo.UniqValues(map1, map2)                              // deduplicated values
```

For sorted keys, use stdlib: `slices.Sorted(maps.Keys(m))`

## HasKey

```go
lo.HasKey(map[string]int{"foo": 1}, "foo")   // true
```

## ValueOr

Safe lookup with default.

```go
lo.ValueOr(m, "missing-key", 42)   // 42
```

## MapKeys

Transform keys to a different type.

```go
lo.MapKeys(map[int]int{1: 10, 2: 20}, func(v int, k int) string {
    return strconv.Itoa(k)
})
// map[string]int{"1": 10, "2": 20}
```

**Err:** `lo.MapKeysErr`

## MapValues

Transform values to a different type.

```go
lo.MapValues(m, func(v int, k string) string {
    return strconv.Itoa(v)
})
```

**Err:** `lo.MapValuesErr`

## MapEntries

Transform both keys and values simultaneously.

```go
lo.MapEntries(map[string]int{"foo": 1, "bar": 2}, func(k string, v int) (int, string) {
    return v, k
})
// map[int]string{1: "foo", 2: "bar"}
```

**Err:** `lo.MapEntriesErr`

## MapToSlice

Convert map to slice via transform.

```go
lo.MapToSlice(map[string]int{"a": 1, "b": 2}, func(k string, v int) string {
    return fmt.Sprintf("%s=%d", k, v)
})
// []string{"a=1", "b=2"}
```

**Err:** `lo.MapToSliceErr`

## FilterMapToSlice

Map to slice, only including matching entries.

```go
lo.FilterMapToSlice(m, func(k string, v int) (string, bool) {
    return k, v > 1
})
```

**Err:** `lo.FilterMapToSliceErr`

## FilterKeys / FilterValues

Filter a map, returning just the matching keys or values as a slice.

```go
lo.FilterKeys(m, func(k string, v int) bool { return v > 1 })     // []string{...}
lo.FilterValues(m, func(k string, v int) bool { return k == "a" }) // []int{...}
```

**Err:** `lo.FilterKeysErr`, `lo.FilterValuesErr`

## PickBy / OmitBy

Filter map entries, returning a new map.

```go
// Keep entries matching predicate
lo.PickBy(m, func(k string, v int) bool { return v > 1 })

// Remove entries matching predicate
lo.OmitBy(m, func(k string, v int) bool { return v > 1 })
```

**Err:** `lo.PickByErr`, `lo.OmitByErr`

## PickByKeys / PickByValues / OmitByKeys / OmitByValues

Filter by specific keys or values.

```go
lo.PickByKeys(m, []string{"foo", "baz"})     // subset with those keys
lo.OmitByKeys(m, []string{"foo", "baz"})     // everything except those keys
lo.PickByValues(m, []int{1, 3})              // subset with those values
lo.OmitByValues(m, []int{1, 3})              // everything except those values
```

## Assign

Merge maps left-to-right (last wins on conflict).

```go
lo.Assign(
    map[string]int{"a": 1, "b": 2},
    map[string]int{"b": 3, "c": 4},
)
// map[string]int{"a": 1, "b": 3, "c": 4}
```

Note: for simple two-map merge, `maps.Copy(dst, src)` from stdlib also works.

## Entries / FromEntries

Convert between map and `[]lo.Entry[K, V]` (each has `.Key`, `.Value`).

```go
entries := lo.Entries(map[string]int{"a": 1, "b": 2})
// []lo.Entry[string, int]{{Key: "a", Value: 1}, {Key: "b", Value: 2}}

m := lo.FromEntries(entries)
// map[string]int{"a": 1, "b": 2}
```

Aliases: `lo.ToPairs` / `lo.FromPairs`

## Invert

Swap keys and values.

```go
lo.Invert(map[string]int{"a": 1, "b": 2})
// map[int]string{1: "a", 2: "b"}
```

## ChunkEntries

Split a map into a slice of smaller maps.

```go
lo.ChunkEntries(map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}, 3)
// []map[string]int{{"a": 1, "b": 2, "c": 3}, {"d": 4}}
```
