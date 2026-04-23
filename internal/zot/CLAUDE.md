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
- **Writes** → `internal/zot/api` calls the Zotero Web API. The **`api.Writer`** interface captures every write method (items, notes, collections, tags, attachments — see `writer.go`). Consumer-side narrow interfaces (`extract.NoteWriter`, `fix.CitekeyWriter`, `pdffind.Attacher`) slice `Writer` further for testability. We do **NOT** wait for local sync-back — Zotero desktop listens on its own sync stream and updates `zotero.sqlite` automatically. API success = done from our side.

**Corollary:** immediately after a write the local DB will briefly diverge from the server. That's fine for humans with Zotero desktop running; it's a real problem for headless agents. Two escape hatches exist for that case — both opt-in, both keep the Writer interface pure:

- **Hydrated writes.** `CreateItem` and `CreateCollection` return the full `*client.Item` / `*client.Collection` (Zotero's `successful[0]` response slot already carries it — no extra GET). `WriteResult.Data` threads this through to `--json` output so agents see what they just created without a readback. Downstream write paths (`CreateChildNote`, `CreateChildAttachment`) only need the key and extract `.Key`.
- **Exported API reads** for the `--remote` CLI flag on `item read`, `item list`, `collection list`, and `search`. `api.Client.GetItem`, `ListItems`, `ListCollections` are exported methods on `*Client` — **not on the `Writer` interface**. They convert to `local.Item` / `local.Collection` via `api.ItemFromClient` / `CollectionFromClient` so consumers see one uniform shape regardless of source. `search --remote` passes `qmode=everything` so it matches abstract + fulltext + notes (local search is title/DOI/publication/creators only). Prefer `local.Reader` for normal reads (fast, no rate limit); use `--remote` when the local DB is stale or for agent flows that need ground truth.
- **Silent API fallback on bulk collection add.** `collection add --from-file` originally looked up every key in local SQLite to avoid per-item GETs; keys missing locally were reported as failed. `resolveBulkCollectionAddItems` now calls `api.GetItem` for each local-miss so agent workflows that pipe keys straight from `item add` work. The fast path (no API reads) is preserved when every key is local.

**Adding new operations:** writes go on `api.Writer` (and `*Client`). Reads go on `local.Reader` (and `*local.DB`). **Never add read methods to `Writer`.** API-side read methods belong on `*Client` directly (like `GetItem`, `ListCollections`, `ListGroups`) and stay out of any interface — the `--remote` and hydration flows are the only callers, and they're explicit at the call site.

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

## PDF discovery & attach (`internal/zot/pdffind/`)

OpenAlex-led lookup for Zotero items missing their PDFs. Lives under `zot doctor pdfs` — scoped to a collection (default `missing-pdf`) rather than library-wide, because this is user-curated triage, not hygiene. Read-only by default; `--download` and `--attach` are opt-in writes.

- **Scan** (`pdffind.Scan`): one HTTP call per item — `ResolveWork` by DOI when present (deterministic), else `SearchWorks` by title (top hit; flagged `LookupMethod="title"` so the renderer can surface the lower confidence). Emits one `Finding` per input item with PDF URL, landing-page URL, OpenAlex DOI, OA status, and `FallbackURLs` (friendly-host-ranked alternates for when `best_oa_location` is a paywall but `locations[]` has an arxiv/PMC copy).
- **Download** (`pdffind.Download`): parallel HTTP GETs with content-type guardrails that reject HTML paywalls masquerading as PDFs. UA is the honest `sci-zot (+…)` — commercial publishers block it, friendly hosts prefer it. See `download.go` header comment for the tradeoff.
- **Attach** (`pdffind.Attach`): drives `api.CreateChildAttachment` + `api.UploadAttachmentFile` per downloaded finding. Serial (Zotero rate-limits aggressively; two API calls + external S3 per item). Per-item failures record `AttachError` and the batch continues. On upload failure after create succeeds, `AttachmentKey` stays populated so the "created but not uploaded" state is surfaceable.

Cache: scan results at `os.UserCacheDir()/sci/zot/pdffind/`. Survives across runs; `--refresh` re-queries, `--no-cache` disables the current run entirely.

## Desktop connector path (`internal/zot/connector/`) — `zot import`

`zot import <path>` is the drag-drop equivalent: it POSTs to Zotero desktop's local server on `127.0.0.1:23119/connector/saveStandaloneAttachment`, then calls `/connector/getRecognizedItem` to await the same metadata-recognition pipeline desktop runs for drag-drop (first-page-text → Zotero recognizer service → CrossRef/arXiv → parent bib item). Requires desktop to be running; exempt from `--library` because desktop writes to whichever library is currently selected in its UI.

Wire-format landmines learned the hard way (source: `chrome/content/zotero/xpcom/server/server_connector.js` on `zotero/zotero` main):

- **saveStandaloneAttachment demands Content-Length.** Chunked transfer gets a 400 "Content-length not provided" back. Go's `http.NewRequest` only sets Content-Length automatically for `*bytes.Reader` / `*bytes.Buffer` / `*strings.Reader`; a `*os.File` body falls through to chunked. `SaveStandaloneAttachment` buffers the full body into `[]byte` + `bytes.NewReader` and sets `req.ContentLength` explicitly. PDFs are a few MB; streaming huge uploads would need a different approach.
- **getRecognizedItem blocks server-side** on `await session.autoRecognizePromise`. It is NOT a polling endpoint — one synchronous call waits until recognition completes. A ctx deadline is the only timeout knob.
- **getRecognizedItem returns only `{title, itemType}` on success.** The Zotero itemKey is stripped out by the handler (`jsonItem = {title: item.getDisplayTitle(), itemType: item.itemType}`). On "recognition finished with no match" it returns **204 No Content**. Surfacing the parent itemKey would require a separate library lookup (by title, or by querying items added since a pre-upload version snapshot); v1 doesn't do this — the user finds the created item via `zot search <title>` if they want the key.
- **Browser-guard bypass.** `server.js` flags any `User-Agent: Mozilla/…` as a browser and demands additional CSRF headers (`server.js:407–424`). We send a non-Mozilla UA (`sci-zot-connector`) and also set `X-Zotero-Connector-API-Version: 3` for belt-and-suspenders — real browser connectors send the version header and it silences the guard.

The endpoint is undocumented for third-party use. Zotero maintainers have called it "not really intended for external consumption" — treat it as tolerated-but-unofficial. If desktop changes `getRecognizedItem`'s response shape, the `TestGetRecognizedItem_*` tests in `connector/client_test.go` are the regression line.

## Zotero file upload — 4-phase dance (`internal/zot/api/files.go`)

Creating an `imported_file` attachment with actual bytes is a 4-call sequence. Canonical spec is the top-level description in `~/Documents/webapps/apis/zotero/openapi.yaml`.

1. `CreateChildAttachment` — `POST /items` creating an `imported_file` child (reuses `CreateItem`).
2. `requestUploadAuth` — `POST /items/{key}/file` form (`md5`, `filename`, `filesize`, `mtime` [epoch millis]) with `If-None-Match: *`. Response is either `UploadAuth` (pre-signed S3 params) OR `{"exists": 1}` (server-side dedup hit). The `oneOf` has no discriminator, so we peek at `exists` before decoding. The exists case returns the `errUploadExists` sentinel and skips phases 3–4.
3. `uploadToS3` — multipart POST to `auth.URL` with `auth.Params` as form fields and a `file` field whose body is **`prefix + fileBytes + suffix`** with `Content-Type: auth.ContentType`. Plain `net/http.DefaultClient` — must NOT go through `retryDoer` (which injects Zotero auth headers and would collide with the pre-signed policy).
4. `registerUpload` — `POST /items/{key}/file` form (`upload=<uploadKey>`) with `If-None-Match: *`. Expects **204 No Content**.

**Union bypass:** the generated `UploadFileFormdataRequestBody` is a `union json.RawMessage` (oapi-codegen's rendering of the spec's `oneOf UploadAuthRequest | UploadRegisterRequest`), unusable without unsafe reflection. Both phases 2 and 4 call `UploadFileWithBody` directly with hand-encoded `application/x-www-form-urlencoded` bytes. Don't try to unwind the union — it'd be more code than the bypass.

**Orchestrator** (`UploadAttachmentFile`) wires 2→4 and short-circuits on `errUploadExists`. On phase-3 failure, phase 4 is deliberately NOT called — the attachment item already exists on Zotero without bytes, and the caller's renderer reports "created, not uploaded" so the user can retry or clean up.

**Buffering:** the whole file is read into `[]byte` before hashing. PDFs in practice are a few MB; revisit if a caller ever needs to stream multi-GB payloads.

## Conventions

- **Raw `database/sql` in `local/`** — same exception family as `dbtui`. Local reads are perf-sensitive and don't need dbx ergonomics.
- **All inputs validated at the command layer.** `internal/zot.Setup()` expects pre-validated args; interactive prompting and `--json` non-interactive validation both live in `cli/setup.go`.
- **Every write command short-circuits via `requireAPIClient(ctx)`** — reads the library scope from ctx, checks `RequireConfig()` + `netutil.Online()`, and builds the API client with `WithLibrary(ref)`. Destructive ops go through `cmdutil.ConfirmOrSkip` with `--yes` bypass.
- **`--library` is required on every command except `setup` and `info`** — the persistent flag is wired into both entry points via `cli.PersistentFlags()` + `cli.ValidateLibraryBefore`. Setup configures both libraries at once; info summarizes both when the flag is absent.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--user-id` when `--json` is set. Any new prompting command must do the same check.

## Saved searches (`/searches`)

Zotero's saved searches are a parallel surface to collections: named virtual
queries with `{condition, operator, value}` triples. Exposed via
`zot saved-search {list,show,create,update,delete}`. Same API shape as
collections — `POST /searches` returns a `MultiObjectResult` and hydrates
`successful[0]` (so `create` returns the full object without a follow-up
GET). Pagination + 412-retry work the same way as collections.

- **Update is full replacement.** There's no single-search PATCH endpoint.
  `UpdateSavedSearch` sends the whole `{name, conditions}` payload via
  `POST /searches` with `key+version`. The CLI's `update` command reads the
  existing record first so `--name` alone or `--condition` alone work.
- **Pseudo-conditions.** `joinMode` (AND→OR), `noChildren`, and
  `includeParentsAndChildren` are conditions that modify the search rather
  than filter it. The CLI's `--any` flag prepends `joinMode:any:` for
  convenience; the rest must be hand-specified.
- **`--condition` intentionally omits `Local: true`.** See "slice-flag Local
  quirk" below — this is a waiver, not an oversight.

## Slice-flag Local quirk (urfave/cli v3)

**Bug:** `cli.StringSliceFlag` (and every other slice flag type) with
`Local: true` keeps only the LAST `--flag X` occurrence on the command line.
`--tag a --tag b --tag c` yields `[c]`, not `[a,b,c]`.

**Why:** urfave/cli v3's `FlagBase.Set` re-runs `PreParse` on every `Set`
call when the flag is `Local`, and `SliceBase.Create` zeroes the underlying
slice in `PreParse`. The accumulated values are wiped before the new value
is appended. Reading via `cmd.StringSlice(name)` is equally broken — the
underlying storage is the same.

**Fix:** drop `Local: true` for slice flags. A `// lint:no-local` waiver
right before the flag literal satisfies the lint-guard rule. The flag still
won't leak in practice because every slice-flag site is on a leaf command.
`Destination` continues to work correctly when `Local` is off.

**Regression test:** `internal/zot/cli/sliceflag_quirk_test.go` reproduces
the bug AND exercises every production slice flag (`item add --tag/--author`,
`item note add --tag`, `find works --filter`, `llm query --key`,
`doctor citekeys --kind/--item`, `saved-search {create,update} --condition`)
to prevent regressions. If the reproduction test ever starts passing with
`Local: true`, urfave/cli has fixed the upstream bug and the waivers can be
removed.

**Orthogonal gotcha:** urfave/cli's default slice separator is `,`, so
`--author "Smith, A"` still splits into `["Smith", " A"]`. Not fixed by the
Local workaround — callers must pre-escape or pass `Name` without commas.

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
- **Config schema migration**: `LoadConfig` silently rewrites pre-rename `library_id` → `user_id`. When adding a new field rename, extend `migrateLegacyConfig` in `config.go` with the new mapping and add a test in `config_test.go` that seeds the legacy shape. Without this, users of the old schema get a misleading "zot not configured" error even though the file exists.
- **`zot find` JSON shape**: `FindWorksResult` / `FindAuthorsResult` emit a **compact** per-entity shape by default — ~12 flat fields per work (openalex_id, doi, title, year, authors[], venue, cited_by_count, oa_status, pdf_url, …) instead of the raw `openalex.Work` which drags full authorships/institutions/ROR graphs. `--verbose` flips `Verbose=true` on the result struct to pass through the raw record. The shape is agent-facing: default to "just enough to rank and pick", opt in to the firehose.
