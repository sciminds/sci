# CLAUDE.md — internal/db

`sci db` verbs and the dbtui-launching `view` command.

## Dual-backend dispatch (load-bearing)

Every public verb in `commands.go` (`Info`, `Create`, `Reset`, `AddCSV`,
`AppendCSV`, `DeleteTable`, `RenameTable`, `RunTUI`) calls `isDuckDB(path)`
first and routes to one of two paths that **share result types** (`InfoResult`,
`MutationResult`, `TableEntry`) but **not** implementations:

- **SQLite** — `sqlite.Open` (`internal/store/sqlite/`, raw `database/sql` over
  modernc.org/sqlite).
- **duckdb** — `internal/duck`, which shells out to the `duckdb` CLI (a
  **required** dep, in `internal/doctor/Brewfile`).

There is deliberately **no `Backend` interface** — each verb's lifecycle is
one-shot and the dispatch is ~5 lines. Don't add one.

## Rationale lives in godoc (read before editing)

- **`.duckdb` in the TUI** → the native subprocess store at
  `internal/store/duck/`. Row-edit/PK rules, the synthetic-row-ID→PK cache,
  heavy-type rendering (STRUCT/LIST/MAP → compact JSON, `<STRUCT>` / `<MAP[N]>`
  cell summaries), DDL, and import are documented in `store/duck/doc.go`,
  `store.go`, and `heavy.go`.
- **`sci db query` source models** — ATTACHed databases (real table names) vs.
  flat files exposed as the `src` CTE, plus the SQLite `sqlite_all_varchar`
  type-mismatch retry — are documented on `Query` / `queryAttached` /
  `attachForQuery` in `internal/duck/verbs.go`.

## Collision semantics

`AddCSV` errors when a target table already exists (pointing at `sci db
append`); `AppendCSV` errors when the table doesn't exist. Identical on both
backends — see `collisionErr` in `commands.go`.
