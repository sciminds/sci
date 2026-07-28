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

## `content` vs `notes` — an extraction is the paper, not a note

The noun split is load-bearing and the verbs move with it. **`zot content` owns paper text end to end** (`extract`, `refresh`, `list`, `read`, `drop`, `build`, `stats`); **`zot notes` means the notes the user wrote** (`list`, `read`).

Why: extractions are notes only in Zotero's storage sense. Live counts — personal 4,719 notes of which 4,710 are docling; group 421 of which 388. So the old `zot notes list` was a listing of extractions with 9 real notes lost inside it, and `notes delete --all` reached 4,710 things a user would never call notes.

- **Re-scoping the noun without moving the verbs would have been worse than leaving it alone** — `list` showing 42 while `delete --all` still trashed 4,710 is a footgun, not a fix. That's why this landed as one change.
- `local.ListNotes` is the inverse of `local.ListAllDoclingNotes` (`NOT EXISTS` on the docling tag); the two partition the library's notes. It **LEFT-joins the parent** where the docling query inner-joins — standalone notes have no parent and are exactly the ones worth surfacing.
- `DoclingNoteSummary` and `NoteSummary` stay distinct types despite identical fields: the docling shape's `parent_key`/`parent_title` are always present, a real note's are `omitempty`. `queryNoteSummaries` shares the scan so the duplication is in the SQL only.
- Retired paths (`zot extract`, `zot notes add|update|delete`) stay **registered** as `retiredCommand` stubs — urfave would otherwise answer with a bare "command not found", which teaches nothing. Each returns a `CodeUsage` error carrying a rewritten, resubmittable `Fix` via `rewriteCommandFix`. `notes add` → `content extract` appends `--apply`, because `notes add` posted immediately and `content extract` dry-runs by default.

## Relations (`zot link`, `zot link suggest`)

Zotero's "related items" (`dc:relation`) connect a standalone note to the papers it discusses. Reads split the predicates by owner: `Related` is the user's own `dc:relation`, `Other` is Zotero's bookkeeping (`owl:sameAs`, `dc:replaces`) which sci reads but never writes. A PATCH replaces the whole relations object, so `api.patchRelations` must carry every predicate back — see its godoc.

- **Relations live on `local.Item`, snippets don't** — and the difference is *query-derived vs intrinsic*. A search snippet depends on the query (the same item has different snippets per search), so it rides beside the item in `ListResult.Snippets`. Relations are a property of the item, like `Tags`/`Collections`/`Attachments`, which are themselves only populated by `Read` / `api.ItemFromClient` and left empty by `List`/`Search`. This matters because `ItemResult.JSON()` returns the **bare `local.Item`**: anything hung on the result shell instead of the item is invisible under `--json`. The field is a pointer with `omitempty`, so an item with no relations emits a byte-identical shape to before the field existed.
- **One labelling mechanism: `local.ItemLabels`** (one query, not N). Both ends of a relation may be an item or a note, and the two store their name differently; `ItemLabels` returns whichever applies and omits keys with no local row (a relation into another library is normal, not an error). `Read` populates `ItemRelationSet.Titles` from it, and `link list` reads titles off the same field rather than carrying its own map.
- **`item read` without `--remote` will not show a link written seconds ago** — the local mirror lags until Zotero desktop syncs. That's the general reads-local/writes-cloud lag, covered by `staleLocalWarning`; the `item read` guide entry names it explicitly because relations are the case where it bites hardest.
- **`suggest --remote` reads existing relations from the API, and it is not optional in practice.** Every other input (note body, library) is stable in the mirror; *which relations already exist* is the one the mirror is wrong about right after a write. On the live library, `link suggest ZJQMDFWK` read "10 proposed" from the stale mirror and `--remote` read "10 already-linked" — same ten papers. The plan's own end-to-end check only passes with the flag.
- **`link suggest` follows the `fix/` contract** (`internal/zot/link/`): a pure `PlanSuggest` over materialized inputs, `Apply` behind a narrow `Writer`, and ONE `Result` with an `Applied bool` so a dry-run and its apply diff cleanly. Planning subtracts existing relations into `already-linked` (reported, not rewritten — `LinkItems` is idempotent, but reporting is what makes a re-run legible), drops the note's own key (Zotero rejects a self-link), and dedupes by item key merging `Via`.
- **The note body goes through `notemd.HTMLToMarkdown`, never `local.NoteText`** — `NoteText` deletes `<a href="zotero://…">` outright, taking the URI with it. It is the *indexing* path, not a display renderer.
- **`suggest` refuses docling extractions.** Scanning one would walk the *paper's* bibliography — hundreds of references, mostly not in the library, none of them the user's curation. Detected by the `docling` tag, returns `CodeUsage` pointing at `zot content`. Same noun split as `content` vs `notes`.
- **`bib.KindZoteroKey`** is what makes the note's own links resolvable: `bib.ScanText`'s `urlRe` is anchored to `https?://`, so `zotero://` needed its own pass (placed right after wikilinks so it claims its span first). `bib.ResolveRefs` is `Resolve` with the per-reference mapping kept — that's what lets a suggestion report *how* a paper was cited.

## PDF extraction (`internal/zot/extract/`)

Same reads-local / writes-cloud split. See `cli/extract.go`, `cli/notes.go`, `extract/extract.go`. Bulk extraction caches resumable state at `os.UserCacheDir()/sci/zot/extract/`.

**Smoke-test env vars:** `DOCLING=1` (Zotero-mode), `DOCLING_FULL=1` (full-mode), `ZOT_REAL_DB=<dir>`, `DOCLING_PDF`, `ZOT_REAL_CKD_KEY`.

**Two independent stores.** With `extract.dir` set in zot.json, an extraction lands in *both* the Zotero child note and the per-key layout dir (`extract/layout.go`) — and either can be missing on its own. Never gate one on the other: an item with a note but no dir still extracts (artifacts only, no note posted); an item with a dir but no note posts from the layout markdown without running docling (this is the path the 4.8k-dir legacy corpus depends on — a re-point, not a re-extract). Layout resume keys off `.done` + both payload files, **never** the markdown cache, which holds only markdown and can't reproduce the required DoclingDocument JSON.

**The has-markdown invariant.** Every path that creates an extraction note also tags the parent `has-markdown` — best-effort, after the note lands (`Execute`, `ExecuteBatch.postNote`), plus the `--apply` backfill sweep that heals stragglers. `content list` and the saved-search workflow key off that tag, so a create path that skips it makes the extraction invisible.

**`extract.runner=ssh`** delegates the whole command to `extract.host` via `syscall.Exec` (`cli/remote.go`) *before* any local DB or docling work. The remote resolves its own config — per-machine `data_dir` and `extract.dir` — so nothing path-like may cross. Results return through the stores themselves, not the pipe.

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
- **`zot search` ranking & scope**: hits are fetched without a SQL `LIMIT` and ranked in Go (`rankSearchResults` in `local/items.go`) by title relevance (count of positive query words in the title), then content relevance (bm25 from the paper-text index, present only under `--content`), then year desc; the `--limit` slice happens **after** ranking so a broad query can't crowd out the top hit. The base CTE now also lifts `citationKey`, so bare terms and the `@citekey:`/`@key:` field match the stored key **and** the 8-char Zotero item key (a pasted whole synthesized key resolves via its `-ZOTKEY` suffix, `synthKeySuffixRe`). `--content` (`SearchOptions{Content}`) widens **positive free-text** clauses with the paper-content index (see below); field-scoped and negated clauses never consult it. Local-only — mutually exclusive with `--remote`. `local.SearchFulltext` still exists but is no longer on this path: its only caller is dbtui via `view/store.go`.
- **`zot search --export`** shares `runLibraryExport` with `zot export` and now honors `--format bibtex|csl-json`; hydration is one batched `ListAll{Keys}` call (not per-hit `Read`).
- **`zot bib <file-or-dir>`** (`internal/zot/bib/` + `cli/bib.go`): scans markdown/Quarto (`.md`/`.markdown`/`.qmd`) for pandoc `@citekeys`, `[[wikilinks]]` (embeds `![[…]]` skipped), DOIs, arXiv ids, and URLs → resolves against the local library → emits a bibliography of exactly the cited items via the same export pipeline (so cite-keys, the `.zotero-citekeymap.json` sidecar, and drift aliases all apply). **Resolution never guesses**: a ref matching >1 distinct item is reported unresolved with the candidate count; unresolved refs always surface (honesty gate) in `BibResult.Unresolved` and the human footer. `bib` package is pure (no I/O) — `ScanText` + `Resolve`; the CLI owns file walking (`--recursive` skips hidden dirs) and DB access. Cite-key lookup uses `citekey.Resolve`, so a key from `zot export` round-trips even after its synthesized prefix drifts (matched via the `-ZOTKEY` suffix).
- **Paper-content search** (`internal/zot/content/`, `zot search --content`): see that package's `doc.go` for the model — an extraction is the paper's body text, not a note. Rules that live here rather than there:
  - **`--fulltext` and `--note-text` are retired**; `--content` subsumes both and `retiredSearchFlagError` (in `cli/read.go`) rewrites the command line so the error carries a runnable `Fix`. Don't re-add either flag: they were two half-answers (Zotero's word index; a substring scan over note HTML) to one question.
  - **`SearchOptions.Content` receives the group's free text verbatim, quotes intact** (`bareSearchText`, not the word-splitting `positiveQueryWords`). Splitting first would silently downgrade `"prediction error"` from a phrase to two ANDed terms — phrases are most of the point. Field-scoped and negated clauses never consult it.
  - **`--notes` still filters to items that HAVE an extraction** — it does not search inside anything.
  - **Ranking is title-first, bm25-second — not bm25 alone.** `SearchOptions.Content` returns `map[key]score`, not just keys, and `rankSearchResults` uses the score *below* title relevance. Leading with bm25 would bury the paper the user named (items with no extraction have no score at all) under every paper that mentions it in passing; the score's job is the long tail, where every hit ties at zero title words and the old fallback to year meant "newest wins".
  - **Snippets are a second index pass, on purpose** (`content.Index.Snippets`, `Query.Snippets` opt-in). Building an excerpt reads the item's whole body out of `docs`, so the ranking pass (`Index.Scores`, thousands of rows) must not do it — only the ≤`--limit` hits that get displayed. They ride on `ListResult.Snippets` / `ListBriefResult.Snippets` (keyed by item key) rather than inside `local.Item`, so the item shape stays identical across every command. A snippet lookup that fails drops the excerpts, never the search. **The side map is deliberate — don't "fix" it by copying snippets onto each item**; agents join via `data.snippets[item.key]`, and `zot guide`'s contract teaches that join.
  - **A snippet that only restates the title is dropped** (`content.EchoesTitle`, applied by `dropTitleEchoes` in `cli/read.go`). docling opens every extraction with the title as a heading, so a title-word query makes FTS5's best excerpt the title again — on a live `search hyperscanning --content`, three of eight hits spent their snippet line proving nothing. Detection is share-based (≥80% of the snippet's word tokens also in the title), not substring: the real echoes drag along a DOI or a `##` from the front matter, and an exact-subset test lets all of them through. Genuine body prose shares only incidental words and stays.
  - **The provenance header is stripped at index time** (`content.stripProvenance`, gated on a leading `---` fence + `zotero_key:` within `maxProvenanceLines`). It must run *between* `local.UnwrapZoteroDiv` and `local.NoteText` — `NoteText` joins on whitespace, which flattens the YAML block into one line and leaves nothing to recognize. Indexing it cost twice: snippets showed `…UIQV9AXD title: "…" source…` instead of prose, and every paper contained its own title as body text while "docling" matched all 4,376 extractions. Detection is positive-evidence-only, same discipline as `notemd.IsHTMLNote` — guessing wrong eats the top of a paper.
  - **`content.IndexFormat` is the rebuild trigger for indexer changes.** Nothing in a Zotero library moves when sci's own indexing rules do, so the signature can't see it; bump `IndexFormat` whenever indexed text changes shape and `PlanSync` returns `NewRebuildPlan` (every item forced to Update) until the index is restamped. `Stale` returns a `StaleReason` (`StaleLibrary` vs `StaleFormat`) because the two need different explanations — one means the user's library moved, the other means their index is older than the binary.
  - **Every path that finishes a build must call `content.RecordBuilt`** (signature + format). `zot content build` didn't, so the live index carried an empty `meta` table and `Stale` read the missing signature as "never built" — the stale warning was unreachable for as long as it existed.
  - **An item with no extractable text is recorded, not skipped**: `Build` writes an empty-bodied row so the version is on record (otherwise every plan re-plans it forever and `content stats` nags with no action that clears it) and so text the item no longer has stops matching. `Stats` and `Body` both filter `body <> ''`, so coverage reads "4412 of 4413 item(s) with text" rather than claiming the blank one.
  - Staleness is a **warning, never a correctness decision**: `local.ContentSignature` is three cheap aggregates (~10ms) because the accurate answer, `ContentSources`, joins the whole library (~0.5s) and a search can't pay that. `zot content build` always computes the real diff.
  - Live-library numbers (2026-07-26, personal, format 2): 4,412 indexed of 5,159 items — 4,375 docling / 37 Zotero-cache (one extraction is nothing but its provenance header, so it records blank); ~40s full rebuild, ~390MB, queries 30–250ms. The 384 extractions that look "missing" are in the **group** library, which gets its own index (`content.DefaultPath` is per-library).
- **`zot bib --verify`** (`bib/verify.go` + `cli/bib.go` adapters + `internal/zot/doiorg/`): classifies each unresolved ref against upstream indexes — `external` / `not-found` / `ambiguous` / `unchecked` / `error` (godoc on `bib.VerifyStatus` owns the semantics). **The lookup chain is load-bearing, don't collapse it.** `bib.ChainLookup{openAlexLookup, registryLookup}` asks OpenAlex first (rich metadata + `is_retracted`) then doi.org (authoritative existence). OpenAlex alone produced false accusations on the first real-manuscript run — it 404s on monographs (Carey's OSO book) and unindexed preprints, and `not-found` renders as "likely fabricated". Only a registry 404 justifies that verdict. arXiv refs reach the registry via their DataCite DOI (`doiorg.ArxivDOI` → `10.48550/arXiv.<id>`). A transport error must never collapse into `not-found`: `ChainLookup` returns the first real error when no link hits, and `openalex.StatusError` (typed, `errors.AsType`) gates the 404 test on the status code rather than a string sniff.
  - `Unresolved.Candidates` carries the competing Zotero keys on an ambiguity so the fix can name them; `bib.FixCommand` emits an OR-group `search "@key:A | @key:B"` (because `item read` takes one key) and `item add --openalex <doi|W-id>` for externals. No fix for `not-found`/`unchecked` — those need judgement, and the contract reserves `Fix` for verbatim-runnable commands.
  - `bib.trimRefTail` balance-trims a trailing `)` (prose `(doi:10.x/y)` vs. Wiley SICI `10.1002/(SICI)…(19980815)`). Unbalanced parens made real DOIs 404 — under `--verify` that's a false accusation, not a cosmetic bug.
- **`zot find` JSON shape**: `FindWorksResult` / `FindAuthorsResult` emit a **compact** per-entity shape by default — ~12 flat fields per work (openalex_id, doi, title, year, authors[], venue, cited_by_count, oa_status, pdf_url, …) instead of the raw `openalex.Work` which drags full authorships/institutions/ROR graphs. `--verbose` flips `Verbose=true` on the result struct to pass through the raw record. The shape is agent-facing: default to "just enough to rank and pick", opt in to the firehose.

## Reading notes back out (`notemd.HTMLToMarkdown`)

`notemd` is now bidirectional. Without the reverse direction Zotero was a **write-only store** — sci could post a literature note and never cleanly recover it, which blocks every downstream consumer (an agent re-reading its own note, an external index, a bibliography pass). `--md` on `zot notes read` and `zot item note read` adds a `markdown` field; it is **opt-in** so the default `--json` shape stays byte-stable for existing agents (and a 485KB note doesn't double). Human output on both now renders markdown instead of the old tag-stripper, which used to flatten every heading, list marker and link.

**`IsHTMLNote` is the load-bearing part — don't simplify it to "convert everything".** In the live library **5,098 of 5,140 notes are raw markdown inside Zotero's wrapper div**, not HTML: the docling extraction path posts markdown by default (`--html` is opt-in), and Zotero just wraps whatever it's given. Running those through an HTML→markdown converter is destructive — HTML collapses whitespace, so the YAML provenance block at the top of every extraction note becomes one unparseable line, and `zotero_key` comes back `zotero\_key`.

Detection defaults to HTML (that's what Zotero declares the field to be) and escapes to markdown only on positive evidence, in order:

1. A block-level tag **opening** the body → HTML. Testing what opens it, not what appears anywhere, is deliberate: docling markdown embeds HTML tables mid-document and is still markdown.
2. Otherwise markdown only if a markdown block construct is present (frontmatter fence, ATX heading, list, code fence). Without this a body of inline-only HTML (`<b>bold</b> and <i>italic</i>`) has no leading block tag and would pass through as raw tags.

Round trip is pinned over exactly the sanitizer's tag vocabulary, plus `zotero://select/...` URIs (Zotero's item-link form — losing those would silently break note→item links). Converter is `html-to-markdown/v2` with base+commonmark only, horizontal rule configured to `---` to match what `MarkdownToHTML` round-trips from.

Note the two `local` helpers that predate this and still have their own jobs: `local.UnwrapZoteroDiv` (wrapper strip for the mq/`llm read` path) and `local.NoteText` (`notetext.go` — tags→text, now the *indexing* path for `internal/zot/content`, not display).
