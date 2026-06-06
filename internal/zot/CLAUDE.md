# CLAUDE.md — zot (internal/zot/)

Zotero library management, mounted under `sci zot`.

For the package layout, command tree, and type definitions, read the source — `cli/cli.go`, `result.go`, `hygiene/hygiene.go` are the entry points. The CLI tree lives in `internal/zot/cli.Commands()` (its own package for a testable boundary), mounted by `cmd/sci/zot.go`.

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

`internal/zot/client` is generated from the Zotero OpenAPI spec via `just zot-gen`. Provenance, the user→group path-mirroring transform, and the add-an-endpoint workflow are documented in `client/doc.go`. **Never hand-edit `zotero.gen.go`** — needed surface goes through `internal/zot/api`.

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

OpenAlex-led lookup for Zotero items missing their PDFs. Lives under `zot doctor pdfs`. Read-only by default; `--download` and `--attach` are opt-in writes.

**Item source flags** (mutually exclusive):

- **`--collection NAME|KEY`** — local SQLite, default `missing-pdf`. Fast but stale if Zotero desktop hasn't synced.
- **`--saved-search NAME|KEY`** — live via the Zotero Web API. The `internal/zot/savedsearch` package translates supported conditions (`tag is/isNot`, `itemType is/isNot`, `collection is`, `noChildren=true`) into API filter params (`?tag=`, `?itemType=`, `/items/top`, etc.). Saved-search conditions outside that set error with the offending clauses listed — silently dropping them would produce results that don't match what desktop renders. Zotero's Web API ignores `?searchKey=` on item endpoints, so we project conditions ourselves; `joinMode` and `includeParentsAndChildren` modifiers at their default values are silently ignored (non-default `joinMode=any` would need per-condition fan-out).
- **`--keys-from FILE|-`** — explicit 8-char keys, one per line (blanks and `#` comments skipped). Resolved via the API in batches of 50 (the `?itemKey=` cap).

The saved-search and keys-from paths use `api.ListItems` with the new `Tag`, `Top`, and `ItemKeys` fields on `ListItemsOptions` (all 8 dispatch combinations of top × collection × user/group are handled in `api/dispatch.go`).

**DOI normalization on 404**: `pdffind.lookupOne` retries against `doi.StripSubobject(item.DOI)` when the original DOI 404s and matches a known publisher subobject pattern (Frontiers `/abstract`+`/full`, PLOS `.tNNN`/`.gNNN`/`.sNNN`, PNAS `/-/DCSupplemental*`). Successful retries surface as `LookupMethod="doi-normalized"` with the parent DOI in `Finding.NormalizedDOI`. The patterns live in `internal/zot/doi/` and back the `zot doctor dois` repair surface (which rewrites the stored DOI through the Web API). See `internal/zot/doi/` for the regex catalog.

- **Scan** (`pdffind.Scan`): one HTTP call per item — `ResolveWork` by DOI when present (deterministic), else `SearchWorks` by title (top hit; flagged `LookupMethod="title"` so the renderer can surface the lower confidence). Emits one `Finding` per input item with PDF URL, landing-page URL, OpenAlex DOI, OA status, and `FallbackURLs` (friendly-host-ranked alternates for when `best_oa_location` is a paywall but `locations[]` has an arxiv/PMC copy).
- **Download** (`pdffind.Download`): parallel HTTP GETs with content-type guardrails that reject HTML paywalls masquerading as PDFs. UA is the honest `sci-zot (+…)` — commercial publishers block it, friendly hosts prefer it. See `download.go` header comment for the tradeoff.
- **Attach** (`pdffind.Attach`): drives `api.CreateChildAttachment` + `api.UploadAttachmentFile` per downloaded finding. Serial (Zotero rate-limits aggressively; two API calls + external S3 per item). Per-item failures record `AttachError` and the batch continues. On upload failure after create succeeds, `AttachmentKey` stays populated so the "created but not uploaded" state is surfaceable.

Cache: scan results at `os.UserCacheDir()/sci/zot/pdffind/`. Survives across runs; `--refresh` re-queries, `--no-cache` disables the current run entirely. Cache key is `doi:<original-DOI>` (or `title:<title>`); after a `doctor dois --fix --apply` patches stored DOIs, stale `doi-normalized` entries should be invalidated so the rerun cache-hits land on the parent DOI under its new key.

## `zot import` — Zotero desktop connector (`internal/zot/connector/`)

`zot import <path>` drag-drops a PDF into the running Zotero desktop via its local connector server (the metadata-recognition pipeline runs too). Exempt from `--library` — desktop writes to whichever library its UI has selected. The undocumented wire-format landmines (Content-Length vs chunked, the 204 "no match", the non-Mozilla UA that dodges the CSRF guard, the stripped item key) are documented on the `internal/zot/connector` package + `client.go` godoc — read that before touching it. `connector/client_test.go` is the regression line if desktop's response shape changes.

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
