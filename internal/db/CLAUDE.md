# CLAUDE.md — internal/db

`sci db` verbs and the dbtui-launching `view` command.

## Dual-backend dispatch (load-bearing)

Every public verb in `commands.go` (`Info`, `Create`, `Reset`, `AddCSV`,
`AppendCSV`, `DeleteTable`, `RenameTable`, `RunTUI`) checks `isDuckDB(path)`
at the top and routes to either:

- The SQLite path — opens via `sqlite.Open` from `internal/store/sqlite/`
  (raw `database/sql` over modernc.org/sqlite). Implements `store.DataStore`.
- The duckdb path — calls into `internal/duck`, which shells out to the
  `duckdb` CLI. duckdb is a **required** dependency (in
  `internal/doctor/Brewfile`).

The two paths share types (`InfoResult`, `MutationResult`, `TableEntry`)
but not implementations. There's no `Backend` interface — each verb's
lifecycle is one-shot and the dispatch is ~5 lines.

## RunTUI on .duckdb

`.duckdb` files open through a native subprocess-backed store at
`internal/store/duck/` — `duckstore.Open(path)` spawns
`duckdb -readonly -jsonlines <path>` and serves dbtui's reads off that
long-running process. dbtui runs with `WithReadOnly()` because the
backend is read-only in Phase 1: every mutation method returns
`store.ErrReadOnly`. No tempfile mirror, no size cap.

**Type fidelity:** STRUCT/LIST/MAP currently render as compact JSON
strings in cells (their jsonlines on-wire form). Phase 2 will add
pretty-print rendering in the preview overlay.

**Phase 3** (editable native backend) is deliberately deferred. The
hooks (`RowIdentifier.PKValues`, mutation methods returning
`ErrReadOnly`) are in place so a follow-up PR can light them up without
churning callers.

## Collision semantics

`AddCSV` errors when any target table already exists and the error
mentions `sci db append` as the escape hatch. `AppendCSV` errors when
the target table does not exist. Same behavior on both backends — see
`collisionErr` in `commands.go`.
