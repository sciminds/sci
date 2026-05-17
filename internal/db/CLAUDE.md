# CLAUDE.md — internal/db

`sci db` verbs and the dbtui-launching `view` command.

## Dual-backend dispatch (load-bearing)

Every public verb in `commands.go` (`Info`, `Create`, `Reset`, `AddCSV`,
`AppendCSV`, `DeleteTable`, `RenameTable`, `RunTUI`) checks `isDuckDB(path)`
at the top and routes to either:

- The SQLite path — opens via `data.OpenStore` (pocketbase/dbx over
  modernc.org/sqlite). Existing behavior.
- The duckdb path — calls into `internal/duck`, which shells out to the
  `duckdb` CLI. duckdb is a **required** dependency (in
  `internal/doctor/Brewfile`).

The two paths share types (`InfoResult`, `MutationResult`, `TableEntry`)
but not implementations. There's no `Backend` interface — each verb's
lifecycle is one-shot and the dispatch is ~5 lines.

## RunTUI on .duckdb

dbtui is SQLite-only by contract (see `internal/tui/dbtui/CLAUDE.md`).
`.duckdb` files open by **mirroring** every table into a tempfile SQLite
database via `duck.BuildSQLiteMirror`, then launching dbtui with
`WithReadOnly()`. The title bar shows the original `.duckdb` path so the
user sees what they opened. Tempfile is removed on exit.

**Type fidelity caveat:** duckdb's STRUCT/LIST/MAP/INTERVAL columns
flatten to TEXT in the mirror. Numeric and string columns round-trip
cleanly. Acceptable for v1 — if users hit this we'll add a banner.

## Collision semantics

`AddCSV` errors when any target table already exists and the error
mentions `sci db append` as the escape hatch. `AppendCSV` errors when
the target table does not exist. Same behavior on both backends — see
`collisionErr` in `commands.go`.
