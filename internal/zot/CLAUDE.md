# CLAUDE.md — zot (internal/zot/)

Zotero library management. Also installable standalone via `cmd/zot/` (mirrors the `dbtui` / `markdb` pattern).

**Before writing any slice/map/set transforms, invoke the `lo` skill** to pick the right `lo` or stdlib function. See root `CLAUDE.md` § Modern Go style.

For the package layout, command tree, and type definitions, read the source — `cli/cli.go`, `result.go`, `hygiene/hygiene.go` are the entry points.

## Two surfaces, one command tree

The full urfave/cli v3 tree lives in `internal/zot/cli.Commands()`. Both entry points import it:

- `cmd/zot/main.go` — standalone `zot` binary
- `cmd/sci/zot.go` — `sci zot …` subcommand

Any subcommand added to `internal/zot/cli` shows up in both surfaces automatically. **Never duplicate wiring.**

## Reads local, writes cloud (load-bearing split)

- **Reads** → `internal/zot/local` opens `zotero.sqlite` with `file:…?mode=ro&immutable=1&_pragma=query_only(1)`. Immutable mode skips WAL processing entirely so we never contend with the running Zotero desktop app's locks. All consumers accept the **`local.Reader`** interface (not `*local.DB`), making the read-only contract visible in type signatures. A runtime test (`TestReadOnlyConnection`) verifies INSERT/UPDATE/DELETE/DROP/CREATE are rejected at the SQLite level.
- **Writes** → `internal/zot/api` calls the Zotero Web API. The **`api.Writer`** interface captures the 13 write methods; read helpers (`getItemRaw`, `getCollectionRaw`) are unexported internal implementation details for 412 version-retry. Consumer-side narrow interfaces (`extract.NoteWriter`, `fix.CitekeyWriter`) slice `Writer` further for testability. We do **NOT** wait for local sync-back — Zotero desktop listens on its own sync stream and updates `zotero.sqlite` automatically. API success = done from our side.

**Corollary:** immediately after a write the local DB will briefly diverge from the server. That's fine. Don't add polling or consistency checks.

**Adding new operations:** reads go on `local.Reader` (and `*DB`); writes go on `api.Writer` (and `*Client`). Never add write methods to `Reader` or read methods to `Writer`.

## Generated client (`client/`)

Regenerate with `just zot-gen`. The recipe reads `~/Documents/webapps/apis/zotero/openapi.yaml` (OpenAPI 3.1), rewrites it to 3.0 on the fly because oapi-codegen v2 doesn't support 3.1 unions, then runs `oapi-codegen` and gofmts. If the upstream spec grows new 3.1-isms or name collisions, extend the `sd` pipeline in the justfile rather than hand-editing.

**Never hand-edit `zotero.gen.go`.** Any needed surface goes through `internal/zot/api`.

## 412 Precondition Failed pattern

Zotero uses optimistic concurrency: writes carry `If-Unmodified-Since-Version` (header) or `version` (body), and the API returns 412 if the target advanced.

**Why it's not in middleware:** recovering from a 412 requires re-reading the object to get the fresh version AND rebuilding the request payload — `internal/zot/api/retry.go` only knows HTTP, not object semantics. So 412 recovery is per-operation.

**The helper:** `withVersionRetry(fn, getVersion, initial)` in `items.go`. `fn(ver)` performs the write; on `*VersionConflictError`, `getVersion()` refreshes and `fn` is called once more. More than one retry would indicate a hot-contention loop and we'd rather surface it. Every write op owns its own `getVersion` closure so the refresh path is explicit at the call site.

## Hygiene checks

Four checks (`invalid`, `missing`, `orphans`, `duplicates`) live as sub-commands of `zot doctor`; bare `zot doctor` runs the aggregate. SQL in `local/hygiene.go` + `local/orphans.go`; pure logic (validators, clusterers) in `hygiene/` so they're unit-testable without a DB. Every check returns `*hygiene.Report{Check, Scanned, Findings, Clusters, Stats}`; `Stats` is per-check and read by renderers via type assertion.

**Severity taxonomy** (consistent across checks):

- `SevError` — structurally broken (missing title, attachment file gone from disk)
- `SevWarn` — citation-affecting (missing creators/date, malformed DOI/URL/date, standalone attachment)
- `SevInfo` — coverage gaps and user-workflow choices

**Doctor ordering:** invalid → missing → orphans → duplicates (cheap/structural first). `--deep` enables fuzzy duplicates + `uncollected-item` orphan kind. `--check-files` stays a per-command opt-in (stat's every attachment).

**Opt-in sub-checks** (in `AllOrphanKinds`, not `defaultOrphanKinds`): `orphans --kind uncollected-item`, `orphans --kind missing-file --check-files`.

## PDF extraction (`internal/zot/extract/`)

Same reads-local / writes-cloud split. See `cli/extract.go`, `cli/notes.go`, `extract/extract.go`. Bulk extraction caches resumable state at `os.UserCacheDir()/sci/zot/extract/`.

**Smoke-test env vars:** `DOCLING=1` (Zotero-mode), `DOCLING_FULL=1` (full-mode), `ZOT_REAL_DB=<dir>`, `DOCLING_PDF`, `ZOT_REAL_CKD_KEY`.

## Conventions

- **Raw `database/sql` in `local/`** — same exception family as `dbtui`. Local reads are perf-sensitive and don't need dbx ergonomics.
- **All inputs validated at the command layer.** `internal/zot.Setup()` expects pre-validated args; interactive prompting and `--json` non-interactive validation both live in `cli/setup.go`.
- **Every write command short-circuits via `requireAPIClient()`** — checks `RequireConfig()` + `netutil.Online()` before building the API client. Destructive ops go through `cmdutil.ConfirmOrSkip` with `--yes` bypass.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--library` when `--json` is set. Any new prompting command must do the same check.

## Gotchas

- **Zotero date storage**: `itemDataValues.value` for the `date` field is `"YYYY-MM-DD originalText"` — first token sortable, second is user input. `cleanDate()` strips after the first whitespace for display. Keep raw values in JSON output so downstream tools see authentic data.
- **Zotero date `00` padding**: the sortable form pads unspecified components with `00`, not by truncating. Year-only is `"1871-00-00 1871"`, not `"1871 1871"`. `ValidateDate` treats `month=0`/`day=0` as "unspecified" markers — caught by the real-library smoke test after the first TDD pass flagged 4995 false positives.
- **Schema version drift**: `SchemaOutOfRange()` warns if `version.userdata` is outside `[MinTestedSchemaVersion, MaxTestedSchemaVersion]`. Current tested 120–130 (live DB is 125 as of 2026-04-11). Widen only after verifying every query in `items.go` / `collections.go` / `tags.go`.
- **`tagFilter` vs `tag`**: `DeleteTagsParams.Tag` is a pipe-separated string (`"a || b || c"`), NOT a slice. API caps 50 tags per request — see `DeleteTagsFromLibrary`'s batching.
- **BibTeX export** (`export.go`, `exportlib.go`, `citekey/`): cite-key policy split into `citekey` sub-package to break the `zot → hygiene` import cycle. **Breaking change warning:** `citekey/citekey.go` constants (`wordCount`, `wordMaxLen`, stopword list) define the synthesized key format `{author}{year}-{words}-{ZOTKEY}` — changing any rewrites every key. Drift detection: `.zotero-citekeymap.json` sidecar emits `ids = {oldkey}` aliases.
- **Cite-key fix** (`fix/`): lives in its own subpackage (import cycle: needs both `api` and `citekey`). `zot doctor citekeys --fix` is dry-run by default; `--apply` required to write. Destructive against BBT-managed libraries — confirmation required.
- **`ZOT_REAL_DB` env var** / **`./zotero.sqlite`**: `local/realdb_test.go` uses `ZOT_REAL_DB`. Hygiene real-library tests open `./zotero.sqlite` at the repo root (gitignored) and gate behind `SLOW=1`. Never hardcode the user's live library path.
- **Single-name creators**: institutional authors like "NASA" are stored with `fieldMode=1` and the name in `lastName`. `Creator.Name` carries these; `Creator.First`/`Last` stay empty. BibTeX emits them as `{NASA}` to suppress name parsing.
- **Shared fixture**: `local/fixture_test.go` builds the synthetic `zotero.sqlite` once per `go test` invocation (`sync.Once` + `TestMain`). Adding tables/rows may require updating `TestStats` and `TestListCollections` counts.
