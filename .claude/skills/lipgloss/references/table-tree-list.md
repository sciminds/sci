# Table, Tree & List Sub-Packages

Lipgloss v2 includes three rendering sub-packages for structured data. All are string-in/string-out — they produce styled strings, not interactive widgets.

## Table (`charm.land/lipgloss/v2/table`)

### Quick Start

```go
import "charm.land/lipgloss/v2/table"

t := table.New().
    Headers("Name", "Age", "City").
    Row("Alice", "30", "NYC").
    Row("Bob", "25", "LA").
    Border(lipgloss.RoundedBorder()).
    Width(60)

fmt.Println(t)
```

### Creation & Data

```go
table.New()                          // empty table
t.Headers("Col1", "Col2", "Col3")   // set header row
t.Row("a", "b", "c")                // append one row
t.Rows([]string{"a","b"}, []string{"c","d"})  // append multiple
t.ClearRows()                        // clear all data rows
t.Data(customData)                   // set custom Data interface
```

### Data Interface

For dynamic or large datasets, implement:

```go
type Data interface {
    At(row, cell int) string
    Rows() int
    Columns() int
}
```

Built-in helpers:
- `table.NewStringData(rows ...[]string)` — simple string grid
- `table.Filter(data Data, fn func(row int) bool)` — filtered view
- `table.DataToMatrix(data Data) [][]string` — convert to plain matrix

### Styling

```go
// Base style for all cells
t.BaseStyle(lipgloss.NewStyle().Padding(0, 1))

// Per-cell styling (most flexible)
t.StyleFunc(func(row, col int) lipgloss.Style {
    if row == table.HeaderRow {
        return headerStyle
    }
    if row%2 == 0 {
        return evenRowStyle
    }
    return oddRowStyle
})
```

**Constant:** `table.HeaderRow == -1` — use in StyleFunc to identify the header row.

### Border Control

```go
t.Border(lipgloss.RoundedBorder())   // border style
t.BorderStyle(borderCharStyle)        // style for border characters
t.BorderTop(true)                     // toggle outer edges
t.BorderBottom(true)
t.BorderLeft(true)
t.BorderRight(true)
t.BorderHeader(true)                  // header separator line
t.BorderColumn(true)                  // column separators
t.BorderRow(true)                     // row separators
```

### Sizing & Scrolling

```go
t.Width(80)       // auto-sizes columns to fit
t.Height(20)      // enables virtual scrolling
t.YOffset(5)      // scroll offset (use with Height)
t.Wrap(true)      // enable cell content wrapping (default: true)
```

**Column auto-sizing:** When the table is narrower than `Width`, columns expand evenly. When wider, columns shrink based on median non-whitespace length (smart cropping, not just right-truncation).

### Rendering

```go
s := t.String()    // render to string
s := t.Render()    // alias for String()
fmt.Println(t)     // implements fmt.Stringer
```

---

## Tree (`charm.land/lipgloss/v2/tree`)

### Quick Start

```go
import "charm.land/lipgloss/v2/tree"

t := tree.Root("Project").
    Child("src",
        tree.New().Child("main.go", "util.go"),
    ).
    Child("docs",
        tree.New().Child("README.md"),
    )

fmt.Println(t)
```

Output:
```
Project
├── src
│   ├── main.go
│   └── util.go
└── docs
    └── README.md
```

### Creation

```go
tree.New()                   // empty tree node
tree.Root("label")           // shorthand for New().Root("label")
```

### Building the Tree

```go
t.Root("Project")            // set root label
t.Child("item1", "item2")   // add children (strings, *Tree, Node, fmt.Stringer, []any)
```

**Auto-nesting:** A child tree with no root value becomes a subtree of its preceding sibling:

```go
tree.Root("Root").Child(
    "Parent",
    tree.New().Child("Child1", "Child2"),  // nested under "Parent"
)
```

### Enumerators (Branch Indicators)

```go
t.Enumerator(tree.DefaultEnumerator)  // ├── and └──
t.Enumerator(tree.RoundedEnumerator)  // ├── and ╰──
```

Custom enumerator:
```go
t.Enumerator(func(children tree.Children, i int) (prefix, connector string) {
    if i == children.Length()-1 {
        return "└── ", "    "
    }
    return "├── ", "│   "
})
```

### Indenters

```go
t.Indenter(tree.DefaultIndenter)  // │ for connected, spaces for last
```

### Styling

```go
t.RootStyle(boldStyle)                                   // style root label
t.ItemStyle(dimStyle)                                    // all items
t.ItemStyleFunc(func(children tree.Children, i int) lipgloss.Style { ... })
t.EnumeratorStyle(grayStyle)                             // branch indicators
t.EnumeratorStyleFunc(func(children tree.Children, i int) lipgloss.Style { ... })
t.IndenterStyle(grayStyle)                               // indent lines
t.IndenterStyleFunc(func(children tree.Children, i int) lipgloss.Style { ... })
```

### Display Control

```go
t.Hide(true)              // hide this node
t.Offset(2, 1)            // show children[2:len-1] (pagination)
t.Width(40)               // pad items to fill width
```

### Interfaces

If you need custom data sources:

```go
type Node interface {
    Value() string
    Children() Children
    Hidden() bool
    SetHidden(bool)
    SetValue(any)
}

type Children interface {
    At(int) Node
    Length() int
}
```

---

## List (`charm.land/lipgloss/v2/list`)

Lists are thin wrappers around `tree.Tree` — same API patterns, different default enumerators.

### Quick Start

```go
import "charm.land/lipgloss/v2/list"

l := list.New("First item", "Second item", "Third item")
fmt.Println(l)
```

Output:
```
• First item
• Second item
• Third item
```

### Creation & Items

```go
list.New("a", "b", "c")     // create with items
l.Item("new item")           // add single item
l.Items("x", "y", "z")      // add multiple items
```

**Nested lists:** Pass a `*List` as an item:
```go
sub := list.New("Sub 1", "Sub 2")
l := list.New("Top 1", sub, "Top 2")
```

### Built-in Enumerators

```go
l.Enumerator(list.Bullet)     // • (default)
l.Enumerator(list.Dash)       // -
l.Enumerator(list.Asterisk)   // *
l.Enumerator(list.Arabic)     // 1. 2. 3.
l.Enumerator(list.Alphabet)   // A. B. C. ... AA. AB.
l.Enumerator(list.Roman)      // I. II. III. IV.
```

### Styling

Same pattern as Tree:

```go
l.ItemStyle(style)
l.ItemStyleFunc(func(items list.Items, i int) lipgloss.Style { ... })
l.EnumeratorStyle(style)
l.EnumeratorStyleFunc(func(items list.Items, i int) lipgloss.Style { ... })
l.IndenterStyle(style)
```

### Display Control

```go
l.Hide(true)
l.Offset(start, end)   // show items[start:len-end]
```

---

## Decision Tree: Which Sub-Package?

```
Structured tabular data with headers?
  → table

Hierarchical parent-child relationships?
  → tree

Flat enumerated list (bullets, numbers)?
  → list

Need interactive selection/scrolling?
  → These are renderers only. Use bubbles/list or bubbles/table
    for interactive widgets, then style with lipgloss.
```
