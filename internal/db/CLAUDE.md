# CLAUDE.md — internal/db

The `sci db` verbs that need dual-backend dispatch (`create`, `reset`, `info`,
`add`, `append`, `delete`, `rename`) and the dbtui-launching `view` command.
The read-only inspection verbs (`query`, `table`, `cols`, `head`, `tail`,
`glimpse`, `shape`, `summarize`, `convert`) live in `cmd/sci/db.go` and dispatch
straight to `internal/duck` — they never enter this package.

## Dual-backend dispatch (load-bearing)

Every public verb in `commands.go` (`Info`, `Create`, `Reset`, `AddCSV`,
`AppendCSV`, `DeleteTable`, `RenameTable`) routes on `isDuckDB(path)` to one of
two paths that **share result types** (`InfoResult`, `MutationResult`,
`TableEntry`) but **not** implementations:

- **SQLite** — `sqlite.Open` (`internal/store/sqlite/`, raw `database/sql` over
  modernc.org/sqlite).
- **duckdb** — `internal/duck`, which shells out to the `duckdb` CLI (a
  **required** dep, in `internal/doctor/Brewfile`).

`RunTUI` is the exception: a **four-way** switch — `isDuckDB` →
`duckstore.Open`, `isParquet` → `duckstore.OpenFileView`,
`sqlite.IsViewableFile` (CSV/TSV/JSON/JSONL) → `sqlite.OpenFileView`, default →
`sqlite.Open`. See its godoc.

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
