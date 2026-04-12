# Set Operations, Search & Membership — Complete Reference

## Membership

### Contains / ContainsBy

```go
lo.Contains([]int{0, 1, 2, 3}, 2)                                     // true
lo.ContainsBy([]int{0, 1, 2, 3}, func(x int) bool { return x == 2 })  // true
```

Note: for simple equality checks, `slices.Contains` from stdlib works too.

### Every / Some / None

Test whether ALL / ANY / NONE of the subset values are in the collection.

```go
lo.Every([]int{0, 1, 2, 3, 4}, []int{0, 2})      // true — all of {0,2} found
lo.Some([]int{0, 1, 2, 3, 4}, []int{0, 99})       // true — at least one found
lo.None([]int{0, 1, 2, 3, 4}, []int{-1, 99})      // true — none found
```

### EveryBy / SomeBy / NoneBy

Predicate-based variants.

```go
lo.EveryBy([]int{1, 2, 3}, func(x int) bool { return x < 5 })   // true
lo.SomeBy([]int{1, 2, 3}, func(x int) bool { return x > 2 })    // true
lo.NoneBy([]int{1, 2, 3}, func(x int) bool { return x < 0 })    // true
```

## Set Operations

### Intersect / IntersectBy

Elements present in ALL input slices.

```go
lo.Intersect([]int{0, 1, 2, 3}, []int{0, 2, 4})               // []int{0, 2}
lo.Intersect([]int{0, 3, 5, 7}, []int{3, 5}, []int{0, 1, 3})  // []int{3} (variadic)
lo.IntersectBy(transform, slice1, slice2)                       // by custom key
```

### Difference

Returns `(leftOnly, rightOnly)`.

```go
left, right := lo.Difference([]int{0, 1, 2, 3, 4}, []int{0, 2, 6})
// left: []int{1, 3, 4}   — in A but not B
// right: []int{6}         — in B but not A
```

### Union

Deduplicated merge of all input slices.

```go
lo.Union([]int{0, 1, 2}, []int{0, 2, 10}, []int{4, 5})
// []int{0, 1, 2, 10, 4, 5}
```

### Without / WithoutBy / WithoutEmpty

Remove specific values.

```go
lo.Without([]int{0, 2, 10}, 2)               // []int{0, 10}
lo.WithoutBy(users, func(u User) int { return u.ID }, 2, 3)  // exclude by ID
lo.WithoutEmpty([]int{0, 2, 10})              // []int{2, 10} — removes zero values
lo.WithoutNth([]int{0, 1, 2, 3}, 1, 3)       // []int{0, 2} — removes by index
```

**Err:** `lo.WithoutByErr`

### ElementsMatch / ElementsMatchBy

Test if two slices contain the same multiset of elements (order-independent).

```go
lo.ElementsMatch([]int{1, 1, 2}, []int{2, 1, 1})   // true
lo.ElementsMatchBy(a, b, func(item T) string { return item.ID() })
```

## Search

### Find / FindOrElse

```go
user, ok := lo.Find(users, func(u User) bool { return u.ID == 42 })
// User{...}, true

user := lo.FindOrElse(users, defaultUser, func(u User) bool { return u.ID == 42 })
```

**Err:** `lo.FindErr`

### FindIndexOf / FindLastIndexOf

```go
val, index, ok := lo.FindIndexOf(items, func(x string) bool { return x == "b" })
// "b", 1, true

val, index, ok := lo.FindLastIndexOf(items, func(x string) bool { return x == "b" })
```

### FindKey / FindKeyBy

Search maps.

```go
key, ok := lo.FindKey(m, "value")                                    // find key for value
key, ok := lo.FindKeyBy(m, func(k string, v int) bool { return v > 5 })
```

### FindUniques / FindDuplicates

```go
lo.FindUniques([]int{1, 2, 2, 1, 3})       // []int{3}        — appear exactly once
lo.FindDuplicates([]int{1, 2, 2, 1, 3})    // []int{1, 2}     — appear more than once
lo.FindDuplicatesBy(items, func(i Item) string { return i.Key })
```

### IndexOf / LastIndexOf

```go
lo.IndexOf([]int{0, 1, 2, 1, 2, 3}, 2)       // 2
lo.LastIndexOf([]int{0, 1, 2, 1, 2, 3}, 2)    // 4
```

### HasPrefix / HasSuffix

Slice prefix/suffix testing (like `strings.HasPrefix` for slices).

```go
lo.HasPrefix([]int{1, 2, 3, 4}, []int{1, 2})   // true
lo.HasSuffix([]int{1, 2, 3, 4}, []int{3, 4})   // true
```

## Min / Max

```go
lo.Min([]int{1, 2, 3})                    // 1
lo.Max([]int{1, 2, 3})                    // 3
lo.MinIndex([]int{1, 2, 3})               // 1, 0
lo.MaxIndex([]int{1, 2, 3})               // 3, 2

lo.MinBy(items, func(a, b Item) bool { return a.Price < b.Price })
lo.MaxBy(items, func(a, b Item) bool { return a.Price > b.Price })
```

**Err:** `lo.MinByErr`, `lo.MaxByErr`, `lo.MinIndexByErr`, `lo.MaxIndexByErr`

## Time-specific Min/Max

```go
lo.Earliest(t1, t2, t3)                   // earliest time.Time
lo.Latest(t1, t2, t3)                     // latest time.Time
lo.EarliestBy(items, func(i Item) time.Time { return i.CreatedAt })
lo.LatestBy(items, func(i Item) time.Time { return i.UpdatedAt })
```

## First / Last / Nth

Safe accessors that never panic.

```go
first, ok := lo.First([]int{1, 2, 3})       // 1, true
last, ok := lo.Last([]int{1, 2, 3})         // 3, true
lo.FirstOrEmpty([]int{})                      // 0
lo.LastOr([]int{}, 42)                        // 42
nth, err := lo.Nth([]int{10, 20, 30}, 1)    // 20, nil
lo.NthOr([]int{10, 20}, 5, -1)              // -1 (fallback)
```
