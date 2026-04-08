# Teatest Protocol

Integration tests using [teatest](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest)
cover the **full message loop**: key → Update() → state change → View().

## When to write a teatest

- A feature adds a new key binding or overlay
- A key triggers a DB mutation (cell edit, row delete, table create/drop)
- A user-facing mode transition is introduced

Do NOT use teatest to re-test pure logic already covered by unit tests in
`app_test.go`, `visual_test.go`, or `tabstate/tabstate_test.go`.

## Checklist for new features

- [ ] Trace dispatch path: `handleKey` → overlay/mode → handler
- [ ] Write teatest covering key → state change (full loop)
- [ ] Use `finalModel` for state, `WaitFor` for async/output, golden for visuals
- [ ] No `time.Sleep` — use `WaitFor` for async, nothing for sync key messages
- [ ] DB mutations verified by querying store directly
- [ ] Read-only variant tested if feature should be blocked on RO tables
- [ ] Test placed in correct file by feature area
- [ ] Golden file added only if a new visual state is introduced
- [ ] `go test ./app/ -run TestTeatest` — all pass, total < 8s
- [ ] `go test ./app/ -run TestTeatest -update` if golden files changed

## File placement

| Feature area | Test file |
|---|---|
| Normal mode keys (sort, pin, column ops, preview) | `teatest_normal_test.go` |
| Edit mode + cell editor | `teatest_edit_test.go` |
| Visual mode (select, yank, paste, delete) | `teatest_visual_test.go` |
| Search overlay | `teatest_search_test.go` |
| Table list overlay | `teatest_tablelist_test.go` |
| Edge cases, read-only, resize | `teatest_edge_test.go` |
| Shared helpers | `teatest_test.go` |

## Test template

```go
func TestTeatest<Feature>(t *testing.T) {
    tm, store := startTeatest(t)   // creates DB + model + waits for render

    sendKey(tm, "x")               // simulate user action
    fm := finalModel(t, tm)        // Ctrl+C → wait → type-assert *Model

    // Assert model state
    if fm.mode != modeNormal { ... }

    // Assert DB state if mutation occurred
    count, _ := store.TableRowCount("table")
}
```

## Helpers (defined in `teatest_test.go`)

| Helper | Purpose |
|---|---|
| `startTeatest(t)` | Setup DB + model + wait for render, returns `(*TestModel, *Store)` |
| `sendKey(tm, "x")` | Send a rune key |
| `sendSpecial(tm, tea.KeyEnter)` | Send a special key |
| `finalModel(t, tm)` | Ctrl+C + wait + type-assert to `*Model` |
| `waitForOutput(t, tm, "text")` | Poll output for substring |
| `waitForTable(t, tm)` | Wait for initial table render |
| `newReadOnlyTeatestModel(t)` | Model with `forceRO=true` |
| `newEmptyTeatestModel(t)` | Model with zero tables |
| `newTeatestModelWithSchema(t, stmts)` | Model with custom SQL schema |

## Golden files

Stored in `testdata/`. Update with:

```bash
go test ./app/ -run TestTeatest -update
```

Only add golden files for distinct visual states (new overlays, new rendering modes).
Golden files contain ANSI escape sequences and are tied to terminal dimensions
(`testTermW=100`, `testTermH=30`).

## Performance

Target: all teatests complete in < 8 seconds. Current: ~3s for ~49 tests.
Key: no `time.Sleep` (except for truly async tab loads), `t.Parallel()` where DBs are isolated.
