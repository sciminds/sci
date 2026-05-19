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
`duckdb -jsonlines <path>` and serves dbtui's reads off that
long-running process. No tempfile mirror, no size cap.

**Row-level mutations** (UpdateCell / DeleteRows / InsertRows) are
backed by a per-table cache of synthetic-row-ID → PK values populated
by `QueryTable`. Update and delete additionally require the target
table to have a PRIMARY KEY (DuckDB has no implicit rowid); tables
without a PK surface in dbtui as `ReadOnly = true` via the optional
`store.RowEditabilityChecker` interface. Insert works regardless.

**Type fidelity:** STRUCT/LIST/MAP currently render as compact JSON
strings in cells (their jsonlines on-wire form); the preview overlay
pretty-prints, syntax-highlights, and type-annotates them (Phase 2,
landed).

**DDL** (RenameTable / DropTable / CreateEmptyTable) issues
`ALTER`/`DROP`/`CREATE` directly through the subprocess.
CreateEmptyTable uses the same `(id INTEGER PRIMARY KEY, name, value)`
default schema as the SQLite backend so new tables are immediately
row-editable.

**Import** (ImportCSV / AppendCSV / ImportFile) routes through
duckdb's native readers — `read_csv_auto` for csv/tsv (TSV passes
`delim='\t'`), `read_json_auto` for json/jsonl/ndjson (line-delimited
variants pass `format='newline_delimited'`). AppendCSV errors when the
target table is missing, matching the SQLite backend's contract;
ImportFile returns `store.ErrImportNotSupported` for unknown
extensions. File paths are funneled through `sqlQuote` so quoted
basenames import safely.

## Collision semantics

`AddCSV` errors when any target table already exists and the error
mentions `sci db append` as the escape hatch. `AppendCSV` errors when
the target table does not exist. Same behavior on both backends — see
`collisionErr` in `commands.go`.
