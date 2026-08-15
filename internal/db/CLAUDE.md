# CLAUDE.md — internal/db

The `sci db` verbs that need dual-backend dispatch (`create`, `reset`, `info`, `add`, `append`, `delete`, `rename`) and the dbtui-launching `view` command. The read-only inspection verbs (`query`, `cols`, `head`, `tail`, `glimpse`, `shape`, `summarize`, `convert`) live in `cmd/sci/db.go` and dispatch straight to `internal/duck` — they never enter this package.

## Dual-backend dispatch (load-bearing)

Every public verb in `commands.go` (`Info`, `Create`, `Reset`, `AddCSV`, `AppendCSV`, `DeleteTable`, `RenameTable`) routes on `isDuckDB(path)` to one of two paths that **share result types** (`InfoResult`, `MutationResult`, `TableEntry`) but **not** implementations:

- **SQLite** — `sqlite.Open` for verbs that write, `sqlite.OpenReadOnly` (via `withReadStore`) for verbs that only look (`internal/store/sqlite/`, raw `database/sql` over modernc.org/sqlite).
- **duckdb** — `internal/duck`, which shells out to the `duckdb` CLI (a **required** dep, in `internal/doctor/Brewfile`).

`RunTUI` is the exception: a **four-way** switch — `isDuckDB` → `duckstore.Open`, `isParquet` → `duckstore.OpenFileView`, `sqlite.IsViewableFile` (CSV/TSV/JSON/JSONL) → `sqlite.OpenFileView`, default → `sqlite.Open`. See its godoc.

## A read verb never changes the file (data safety)

`sqlite.Open` issues `PRAGMA journal_mode = WAL`, which **permanently rewrites** a database that was in the default rollback-journal mode. That is acceptable for a verb the user asked to write and unacceptable for one that only reads, so `Info` goes through `withReadStore` → `sqlite.OpenReadOnly` (mode=ro, `query_only`, immutable when there is no pending WAL) and leaves the file byte-for-byte identical. Any new read-only verb uses `withReadStore`; the regression pin is `TestInfoLeavesTheDatabaseByteIdentical`, which hashes the file before and after — asserting "no error" would not have caught the bug.

`RunTUI`'s SQLite branch still opens writable, because dbtui edits rows: opening `sci view mydb.db` converts a rollback-journal database to WAL before the first keystroke. Fixing that needs a read-only viewer mode in dbtui (or a `--read-only` flag on `sci view`), not a change here.

## Errors carry codes, not driver text

Every user-caused failure is classified in `errors.go` before it leaves this package: `requireDatabase` / `requireInputFile` / `requireTable` and friends return `cmdutil.Coded(...)` values so the `--json` envelope gets a code from the closed vocabulary and the process exits 2 for "rewrite the command line" and 1 for "the work failed". Never let a driver string reach the caller — `SQL logic error: no such table: t (1)` and `PRAGMA journal_mode = WAL: file is not a database (26)` name internals the caller cannot act on. `Fix` is only for a command that can be resubmitted verbatim (e.g. append-to-a-missing-table → the equivalent `sci db add --table …`); everything else is `Try`.

There is deliberately **no `Backend` interface** — each verb's lifecycle is one-shot and the dispatch is ~5 lines. Don't add one.

## Rationale lives in godoc (read before editing)

- **`.duckdb` in the TUI** → the native subprocess store at `internal/store/duck/`. Row-edit/PK rules, the synthetic-row-ID→PK cache, heavy-type handling (STRUCT/LIST/MAP/BLOB columns are replaced server-side with `<STRUCT>` / `<MAP[N]>` / `<FLOAT[N]>` summaries and fetched in full on demand; complex values that do arrive marshal back to compact JSON), DDL, and import are documented in `store/duck/doc.go`, `store.go`, and `heavy.go`.
- **`sci db query` source models** — ATTACHed databases (real table names) vs. flat files exposed as the `src` CTE, plus the SQLite `sqlite_all_varchar` type-mismatch retry — are documented on `Query` / `queryAttached` / `attachForQuery` in `internal/duck/verbs.go`.
