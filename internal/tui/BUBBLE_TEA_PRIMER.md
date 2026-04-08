# Bubble Tea Primer

Bubble Tea is the TUI framework used by all interactive components in sci-go.
If you're new to Go (or to terminal UIs), this explains the core pattern.

## The MVU Pattern (Model-View-Update)

Every Bubble Tea program has three parts:

```go
// 1. Model — a struct that holds ALL application state.
type Model struct {
    cursor int
    items  []string
    err    error
}

// 2. Update — receives a message, returns new state + optional side effect.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "j":
            m.cursor++
        case "q":
            return m, tea.Quit
        }
    }
    return m, nil
}

// 3. View — pure function that renders the model to a string.
func (m Model) View() tea.View {
    var s string
    for i, item := range m.items {
        if i == m.cursor {
            s += "> " + item + "\n"
        } else {
            s += "  " + item + "\n"
        }
    }
    return tea.NewView(s)
}
```

**Key insight:** Update never directly modifies the screen. It returns a new
Model, and Bubble Tea calls View to render it. This separation makes the logic
testable without a real terminal.

## Messages and Commands

A **message** (`tea.Msg`) is any Go value that arrives in Update. Messages
come from:
- Keyboard input (`tea.KeyPressMsg`)
- Window resizes (`tea.WindowSizeMsg`)
- Your own commands (see below)

A **command** (`tea.Cmd`) is a function that performs a side effect and
returns a message:

```go
// A Cmd that loads data from the database.
func loadData(store *Store) tea.Cmd {
    return func() tea.Msg {
        rows, err := store.Query("SELECT * FROM users")
        return dataLoadedMsg{rows: rows, err: err}
    }
}
```

Commands run asynchronously. When they finish, Bubble Tea delivers the
returned message to Update. This is how you do I/O without blocking the UI.

## How to Read the dbtui Code

Start with these files in `internal/tui/dbtui/app/`:

| File | What it does |
|------|-------------|
| `model.go` | The Model struct — all application state lives here |
| `update.go` | Main Update dispatcher — routes messages to handlers |
| `view.go` | Main View — decides what to render based on state |
| `keys.go` | Key binding constants (never bare string literals) |
| `doc.go` | Architecture overview with more detail |

The pattern is always the same: a message arrives in Update, the handler
modifies the Model, and View re-renders. Side effects (database queries,
file I/O) are returned as Cmds that eventually deliver result messages.

## Testing with teatest

The `teatest` library lets you test Bubble Tea programs without a real terminal:

```go
func TestMyFeature(t *testing.T) {
    m := NewModel(store)
    tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))

    // Send keys
    tm.Type("j")                                        // type a character
    tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})        // send special key

    // Wait for output to contain expected text
    teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
        return bytes.Contains(bts, []byte("expected text"))
    })

    // Get final model state
    tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
    final := tm.FinalModel(t).(*Model)

    // Assert on model state
    if final.cursor != 1 {
        t.Errorf("cursor = %d, want 1", final.cursor)
    }
}
```

See `internal/tui/dbtui/app/teatest_*.go` for real examples.
