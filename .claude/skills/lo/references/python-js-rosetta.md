# Python/JS → Go+lo Rosetta Stone

Side-by-side translations for developers coming from Python or JavaScript.

## List Comprehensions / Array.map

**Python:** `[x.upper() for x in names]`
**JS:** `names.map(x => x.toUpperCase())`
**Go+lo:**
```go
lo.Map(names, func(x string, _ int) string {
    return strings.ToUpper(x)
})
```

## Filtered List Comprehension / Array.filter

**Python:** `[x for x in users if x.active]`
**JS:** `users.filter(x => x.active)`
**Go+lo:**
```go
lo.Filter(users, func(u User, _ int) bool {
    return u.Active
})
```

## Filter + Transform (one pass)

**Python:** `[x.name for x in users if x.active]`
**JS:** `users.filter(x => x.active).map(x => x.name)`
**Go+lo:**
```go
lo.FilterMap(users, func(u User, _ int) (string, bool) {
    return u.Name, u.Active
})
```

## Dict Comprehension / Object from entries

**Python:** `{u.id: u for u in users}`
**JS:** `Object.fromEntries(users.map(u => [u.id, u]))`
**Go+lo:**
```go
lo.KeyBy(users, func(u User) int {
    return u.ID
})
```

## Dict Comprehension with transform

**Python:** `{u.id: u.name for u in users}`
**JS:** `Object.fromEntries(users.map(u => [u.id, u.name]))`
**Go+lo:**
```go
lo.SliceToMap(users, func(u User) (int, string) {
    return u.ID, u.Name
})
```

## groupby / _.groupBy

**Python:** `{k: list(v) for k, v in itertools.groupby(sorted(items), key=lambda x: x.dept)}`
**JS:** `_.groupBy(items, x => x.dept)` (lodash)
**Go+lo:**
```go
lo.GroupBy(items, func(i Item) string {
    return i.Department
})
```

## flatMap / chain.from_iterable

**Python:** `[tag for post in posts for tag in post.tags]`
**JS:** `posts.flatMap(p => p.tags)`
**Go+lo:**
```go
lo.FlatMap(posts, func(p Post, _ int) []string {
    return p.Tags
})
```

## reduce / functools.reduce

**Python:** `functools.reduce(lambda acc, x: acc + x.price, items, 0)`
**JS:** `items.reduce((acc, x) => acc + x.price, 0)`
**Go+lo:**
```go
lo.Reduce(items, func(acc int, item Item, _ int) int {
    return acc + item.Price
}, 0)
```

## sum

**Python:** `sum(x.price for x in items)`
**JS:** `items.reduce((a, x) => a + x.price, 0)`
**Go+lo:**
```go
lo.SumBy(items, func(item Item) int { return item.Price })
```

## Counter / frequency map

**Python:** `collections.Counter(tags)`
**JS:** `tags.reduce((m, t) => { m[t] = (m[t]||0)+1; return m }, {})`
**Go+lo:**
```go
lo.CountValues(tags)
// map[string]int{"go": 3, "python": 2}
```

## set() / new Set()

**Python:** `set(ids)`
**JS:** `new Set(ids)`
**Go+lo:**
```go
lo.Keyify(ids)
// map[int]struct{}{1: {}, 2: {}, 3: {}}
```

## list(set(xs)) / [...new Set(xs)]

**Python:** `list(set(xs))`
**JS:** `[...new Set(xs)]`
**Go+lo:**
```go
lo.Uniq(xs)
```

## Unique by key

**Python:** `list({x.id: x for x in items}.values())`
**JS:** `[...new Map(items.map(x => [x.id, x])).values()]`
**Go+lo:**
```go
lo.UniqBy(items, func(i Item) int { return i.ID })
```

## set intersection / difference / union

**Python:** `set(a) & set(b)` / `set(a) - set(b)` / `set(a) | set(b)`
**JS:** (no built-in, use lodash `_.intersection` / `_.difference` / `_.union`)
**Go+lo:**
```go
lo.Intersect(a, b)
lo.Difference(a, b)      // returns (leftOnly, rightOnly)
lo.Union(a, b)
```

## any() / some() / all() / every()

**Python:** `any(x > 5 for x in xs)` / `all(x > 0 for x in xs)`
**JS:** `xs.some(x => x > 5)` / `xs.every(x => x > 0)`
**Go+lo:**
```go
lo.SomeBy(xs, func(x int) bool { return x > 5 })
lo.EveryBy(xs, func(x int) bool { return x > 0 })
lo.NoneBy(xs, func(x int) bool { return x < 0 })
```

## next(x for x in xs if pred(x)) / Array.find

**Python:** `next((x for x in users if x.id == 42), None)`
**JS:** `users.find(x => x.id === 42)`
**Go+lo:**
```go
user, ok := lo.Find(users, func(u User) bool { return u.ID == 42 })
```

## Ternary / conditional expression

**Python:** `"yes" if cond else "no"`
**JS:** `cond ? "yes" : "no"`
**Go+lo:**
```go
lo.Ternary(cond, "yes", "no")
lo.TernaryF(cond, expensiveYes, expensiveNo)  // lazy evaluation
```

## a or b or c / a ?? b ?? c

**Python:** `a or b or c`
**JS:** `a ?? b ?? c`
**Go+lo:**
```go
result, ok := lo.Coalesce(a, b, c)  // first non-zero value
```

## chunks / batched

**Python:** `list(itertools.batched(items, 100))` (3.12+) or `more_itertools.chunked`
**JS:** `_.chunk(items, 100)` (lodash)
**Go+lo:**
```go
lo.Chunk(items, 100)
```

## filter(None, xs) / .filter(Boolean)

**Python:** `list(filter(None, xs))` or `[x for x in xs if x]`
**JS:** `xs.filter(Boolean)`
**Go+lo:**
```go
lo.Compact(xs)  // removes zero-value elements ("", 0, nil, false)
```

## enumerate / .forEach with index

**Python:** `for i, x in enumerate(xs):`
**JS:** `xs.forEach((x, i) => ...)`
**Go+lo:**
```go
lo.ForEach(xs, func(x string, i int) {
    fmt.Printf("%d: %s\n", i, x)
})
```

## zip

**Python:** `list(zip(names, scores))`
**JS:** `names.map((n, i) => [n, scores[i]])`
**Go+lo:**
```go
tuples := lo.Zip2(names, scores)
// []lo.Tuple2[string, int]{{A: "Alice", B: 95}, ...}
```

## dict(zip(keys, values))

**Python:** `dict(zip(keys, values))`
**JS:** `Object.fromEntries(keys.map((k, i) => [k, values[i]]))`
**Go+lo:**
```go
entries := lo.Zip2(keys, values)
m := lo.FromEntries(lo.Map(entries, func(t lo.Tuple2[string, int], _ int) lo.Entry[string, int] {
    return lo.Entry[string, int]{Key: t.A, Value: t.B}
}))
```
Or more directly with `lo.SliceToMap` if you have the paired data.

## sorted(xs, key=lambda x: x.name)

**Python:** `sorted(users, key=lambda u: u.name)`
**JS:** `users.sort((a, b) => a.name.localeCompare(b.name))`
**Go (stdlib):**
```go
slices.SortFunc(users, func(a, b User) int {
    return cmp.Compare(a.Name, b.Name)
})
```

Note: sorting uses stdlib `slices.SortFunc`, not `lo`. The `sort` package is banned.
