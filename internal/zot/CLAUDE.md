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

## Library scope (personal vs. shared)

Every zot command except `setup` and `info` **requires** `--library personal|shared`. The flag is a persistent root-level flag (`cli.PersistentFlags()`) validated by `cli.ValidateLibraryBefore`, which stashes the resolved `zot.LibraryRef` on ctx. `info` optionally takes `--library` to narrow; with no flag it summarizes every library the account has access to.

- **Config** carries both libraries: `UserID` (personal) and `SharedGroupID` + `SharedGroupName` (shared group). Setup auto-detects the shared group via `/users/{userID}/groups` when the account belongs to exactly one; it errors with options listed when the account belongs to ≥2.
- **Resolution** — `Config.Resolve(scope)` maps a scope to a full `LibraryRef{Scope, APIPath, LocalID, Name}`. `Config.ResolveWithProbe(scope, probe)` lazy-detects the shared group on first use if `SharedGroupID` is blank and persists the result.
- **API dispatch** — `api.Client.Lib` drives the switch between `c.Gen.{Op}WithResponse(ctx, UserID, …)` and `c.Gen.{Op}GroupWithResponse(ctx, GroupID, …)`. Generated from the OpenAPI spec + the `scripts/zotero-mirror-paths.yq` transform, so every `/users/{userID}/…` path has a parallel `/groups/{groupID}/…` twin (except `/users/{userID}/groups` itself). `api.New(cfg, api.WithLibrary(ref))` is required — no default; passing nothing errors.
- **Local dispatch** — `local.Open(dir, sel)` accepts a selector: `ForPersonal()` for `type='user'`, `ForGroup(libraryID)` for a specific SQLite libraryID, or `ForGroupByAPIID(groupID)` when you only know the Web API group ID (joins the `groups` table to resolve).
- **CLI plumbing** — `openLocalDB(ctx)` and `requireAPIClient(ctx)` both read the scope from ctx and wire it through. A missing ref in ctx is a hard error — it means the command was registered outside the Before hook.

## Generated client (`client/`)

Regenerate with `just zot-gen`. The recipe reads `~/Documents/webapps/apis/zotero/openapi.yaml` (OpenAPI 3.1), rewrites it to 3.0 on the fly because oapi-codegen v2 doesn't support 3.1 unions, pipes through `scripts/zotero-mirror-paths.yq` to duplicate every `/users/{userID}/…` path as a `/groups/{groupID}/…` twin, then runs `oapi-codegen` and gofmts. If the upstream spec grows new 3.1-isms or name collisions, extend the `sd` pipeline in the justfile rather than hand-editing.

**Never hand-edit `zotero.gen.go`.** Any needed surface goes through `internal/zot/api`.

**Extending to new endpoints:** add the path to the OpenAPI spec (e.g. the spec's `/users/{userID}/groups` → `listGroups`), regenerate, then add the wrapper in `internal/zot/api` that typed callers consume. The yq transform will auto-produce the group-path twin unless the path name suggests it shouldn't (see the `/users/{userID}/groups` skip list in the transform).

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
- **Every write command short-circuits via `requireAPIClient(ctx)`** — reads the library scope from ctx, checks `RequireConfig()` + `netutil.Online()`, and builds the API client with `WithLibrary(ref)`. Destructive ops go through `cmdutil.ConfirmOrSkip` with `--yes` bypass.
- **`--library` is required on every command except `setup` and `info`** — the persistent flag is wired into both entry points via `cli.PersistentFlags()` + `cli.ValidateLibraryBefore`. Setup configures both libraries at once; info summarizes both when the flag is absent.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--user-id` when `--json` is set. Any new prompting command must do the same check.

## Gotchas

- **Zotero date storage**: `itemDataValues.value` for the `date` field is `"YYYY-MM-DD originalText"` — first token sortable, second is user input. `cleanDate()` strips after the first whitespace for display. Keep raw values in JSON output so downstream tools see authentic data.
- **Zotero date `00` padding**: the sortable form pads unspecified components with `00`, not by truncating. Year-only is `"1871-00-00 1871"`, not `"1871 1871"`. `ValidateDate` treats `month=0`/`day=0` as "unspecified" markers — caught by the real-library smoke test after the first TDD pass flagged 4995 false positives.
- **Schema version drift**: `SchemaOutOfRange()` warns if `version.userdata` is outside `[MinTestedSchemaVersion, MaxTestedSchemaVersion]`. Current tested 120–130 (live DB is 125 as of 2026-04-11). Widen only after verifying every query in `items.go` / `collections.go` / `tags.go`.
- **`tagFilter` vs `tag`**: `DeleteTagsParams.Tag` is a pipe-separated string (`"a || b || c"`), NOT a slice. API caps 50 tags per request — see `DeleteTagsFromLibrary`'s batching.
- **BibTeX export** (`export.go`, `exportlib.go`, `citekey/`): cite-key policy split into `citekey` sub-package to break the `zot → hygiene` import cycle. **Breaking change warning:** `citekey/citekey.go` constants (`wordCount`, `wordMaxLen`, stopword list) define the synthesized key format `{author}{year}-{words}-{ZOTKEY}` — changing any rewrites every key. Drift detection: `.zotero-citekeymap.json` sidecar emits `ids = {oldkey}` aliases.
- **Cite-key fix** (`fix/`): lives in its own subpackage (import cycle: needs both `api` and `citekey`). `zot doctor citekeys --fix` is dry-run by default; `--apply` required to write. Destructive against BBT-managed libraries — confirmation required.
- **`ZOT_REAL_DB` env var** / **`./zotero.sqlite`**: `local/realdb_test.go` uses `ZOT_REAL_DB`. Hygiene real-library tests open `./zotero.sqlite` at the repo root (gitignored) and gate behind `SLOW=1`. Never hardcode the user's live library path.
- **Single-name creators**: institutional authors like "NASA" are stored with `fieldMode=1` and the name in `lastName`. `Creator.Name` carries these; `Creator.First`/`Last` stay empty. BibTeX emits them as `{NASA}` to suppress name parsing.
- **Shared fixture**: `local/fixture_test.go` builds the synthetic `zotero.sqlite` once per `go test` invocation (`sync.Once` + `TestMain`). Adding tables/rows may require updating `TestStats` and `TestListCollections` counts. The fixture includes both a user library (`libraryID=1`) and a group library (`libraryID=2, groupID=6506098, name='sciminds'`) so both scopes exercise real rows.
- **Library IDs**: two numbering systems are in play. The **Zotero Web API group ID** (e.g. `6506098`) is what `zot.Config.SharedGroupID` and every `/groups/{groupID}/…` URL carries. The **SQLite `libraries.libraryID`** (e.g. `2` for group, `1` for user) is what `local.*` queries filter on. `local.ForGroupByAPIID` bridges the two via a join on the `groups` table.
