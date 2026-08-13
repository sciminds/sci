# CLAUDE.md — zot (internal/zot/)

The public local read surface over a Zotero library, mounted under `sci zot`.

For the package layout, command tree, and type definitions, read the source — `cli/cli.go`, `cli/moved.go`, `result.go`, `hygiene/hygiene.go` are the entry points. The CLI tree lives in `internal/zot/cli.Commands()` (its own package for a testable boundary), mounted by `cmd/sci/zot.go`.

## The boundary — read this before adding any verb

**sci's Zotero surface is what a public, curl-installed tool can promise any user: read-only operations against their own local library, no credentials, no metered API.** Everything that writes to Zotero, extracts paper text, or asks an upstream index moved to the sibling `zot` binary (2026-08-12, phase 3 of the three-tool reorg). `internal/zot/{pdffind,savedsearch,graph,content,oacache,xrcache,backfill,enrich}` were deleted here; sci keeps no copy.

The dependency arrow is `zot → sci` (`pkg/`) only. **sci never imports or shells `zot`**, so every pointer to a `zot` verb rides in an `Error.Try`, never a `Fix`: zot is not installed on lab machines, and `Fix` is verbatim-runnable or absent.

Three things still speak a network, and the list is closed:

- **`internal/zot/api`** — the `--remote` live reads (`item read`/`list`/`children`, `collection list`, `search`, `link list`), the live-only reads (`item note read`/`list`, `saved-search list`/`show`). The user's own key against the user's own library. **Reads only, as of 2026-08-13**: the package still carries the Web API's write half, because it is the Zotero client and deleting those methods would only mean re-deriving them, but nothing outside the package calls one.
- **`internal/zot/connector`** — localhost, driving the Zotero desktop app the user is already running. `zot import` is the one sanctioned write path, and the app makes it.
- **`setup`** — one `/users/{id}/groups` probe to name the shared group.

**The last Web-API write left on 2026-08-13.** `link add`/`rm`/`suggest --apply` were it, and they MOVED rather than retired because `dc:relation` is real bibliographic data — the sequencing rule held: zot grew the verbs (zot `68b380d`), then sci stubbed its copies, never a gap with zero homes. **`link suggest` is the one verb whose answer changed rather than its address**: zot's proposes work-identity twins out of its corpus, while sci's scanned a NOTE's body for the references it cited. That note-scanning signal was retired deliberately — no `bib` promotion — and the stub says so out loud, because "moved to `zot link suggest`" alone would send someone hunting for a port that was never written. **zot's own DESIGN D29 records the other half of the bargain**: relations are a write surface, never a resolution input, since a `dc:relation` carries no provenance and zot's own writes are byte-identical to a human's.

**No metered third-party index is reachable, and neither is any Zotero write.** `pkg/{openalex,crossref,doiorg}` stay in this repo because the `zot` binary imports them, but nothing under `internal/zot/` does. **`scripts/lint-guard.sh` rule 18 is the fence**, in three halves:

1. `go list -deps ./internal/zot/cli` must not reach `pkg/{openalex,crossref,doiorg}`. A dependency check rather than a grep because the failure is transitive — nothing has to import `pkg/openalex` directly for a `sci zot search` to start spending someone's rate limit.
2. No package under `internal/zot/` outside `{api,cli,client,connector}` may reach `net/http`.
3. **No exported `api.Client` WRITE method may be called outside `internal/zot/api`** — the tightening the link stubs unlocked. It is a grep because `go list` works at package granularity and `api` is one package holding both halves of the API, so "no `net/http` outside {api **reads**, connector}" is a sentence only a method-level check can say. The write list is DERIVED from the method names' leading verb rather than typed out, so a write added next year is fenced the day it lands; what makes that trustworthy is the classification gate in front of it — a method matching neither the write nor the read vocabulary fails the gate rather than slipping through unfenced.

**Retired verbs stay registered** as stubs (`cli/moved.go` for the per-verb list, `cli/retire.go` for the two shapes): `CodeUsage`, the remedy in `Try`, `SkipFlagParsing` so a script still passing the old write flags reaches the explanation instead of an unknown-flag error. A bare "command not found" teaches nothing. **Neither shape ever fills `Fix`** — a moved verb cannot, because `zot` is a different program absent from lab machines; a retired one cannot, because there is no command to hand back at all.

- **`movedToZotCommand`** — the verb lives on in `zot`. A whole namespace retires as ONE leaf stub: with `SkipFlagParsing` the subcommand name arrives as a positional, so `sci zot content read KEY` reaches the Action intact. Only namespaces that KEPT some verbs (`item`, `collection`, `tags`, `saved-search`, `notes`) stub their children one at a time.
- **`retiredOutrightCommand`** — the verb has no home in any binary and the remedy is prose. Today that is exactly `saved-search create`/`update`/`delete`: **the Zotero Web API stores a saved search's definition but cannot evaluate it — only the desktop client runs the query** (the same quirk that once silently no-op'd a pipeline predicate built on one). A write verb for a thing only the desktop can use belongs to the desktop's UI, so the `Try` names Zotero desktop. Writing a plausible `zot saved-search …` string there would be worse than saying nothing: an agent would run it and collect "command not found" from a second tool.

`cli/shrink_test.go` holds both tables. The outright shape has its own assertion — its `Try` must **not** carry the moved shape's "on a machine that has zot installed" sentence, because that routes the caller somewhere with no such verb either.

## Reads local, live reads remote (load-bearing split)

- **Reads** → `pkg/local` opens `zotero.sqlite` with `file:…?mode=ro&immutable=1&_pragma=query_only(1)`. Immutable mode skips WAL processing entirely so we never contend with the running Zotero desktop app's locks. All consumers accept the **`local.Reader`** interface (not `*local.DB`), making the read-only contract visible in type signatures. A runtime test (`TestReadOnlyConnection`) verifies INSERT/UPDATE/DELETE/DROP/CREATE are rejected at the SQLite level; `TestWALConnectionRejectsWrites` pins the same contract against a WAL-mode file, and `TestOpenNeverMutatesTheDatabase` hashes the DB **and** its `-wal` across a real read to prove neither moves.
- **Immutable mode is not free — local reads can be silently stale, and this bit us.** Skipping WAL processing means every change Zotero has committed but not yet checkpointed is invisible. Zotero checkpoints on a clean quit, so the gap is usually empty; while the app is running it can be arbitrarily large. Measured live: a manual dedupe pass left 8 deletions in the WAL, `doctor duplicates` kept reporting the resolved clusters as open, and quitting Zotero made them appear (56 vs 64 rows in `deletedItems` — main file vs main+WAL). **Dropping `immutable=1` is not the fix**: Zotero holds an exclusive lock, so a plain `mode=ro` connection fails to open at all while the app runs (`database is locked`), and one opened while Zotero is closed can start failing mid-run when the user launches it — plus WAL reads make SQLite create a `-shm` inside the user's Zotero directory. Availability wins; the gap is *reported* instead. `local.DB.PendingWAL()` sizes it and `walStaleWarning` (`cli/warnings.go`) turns it into a `stale-local` warning. `TestImmutableOpenCannotSeeUncheckpointedWAL` pins the limitation deliberately — if it fails, someone changed the connection mode and owes this trade a fresh look.
- **Freshness warnings are emitted where the DB is opened, not per command.** `openLocalDBScoped` (`cli/read.go`) prints the WAL caveat to stderr next to the schema-range check, so every command inherits it — `doctor`/`hygiene` never wired up a per-result warning, which is exactly why the staleness went unreported for so long. Result-carried warnings go through `localReadWarnings(db, fix)`, which bundles the mirror-vs-server lag (`staleLocalWarning`, days, fixed by syncing) with the connection-vs-file lag (`walStaleWarning`, bytes, fixed by letting Zotero checkpoint). Call sites take both or neither.
- **`--remote`** → `internal/zot/api` calls the Zotero Web API. `api.Client.GetItem`, `ListItems`, `ListCollections`, `ListNoteChildren` are exported methods on `*Client` and convert to `local.Item` / `local.Collection` via `api.ItemFromClient` / `CollectionFromClient`, so consumers see one uniform shape regardless of which plane answered. `search --remote` passes `qmode=everything` so it matches abstract + fulltext + notes (local free-text search matches metadata fields — see the `zot search` gotcha). Prefer `local.Reader` for normal reads (fast, no rate limit); use `--remote` when the local DB is stale or an agent needs ground truth.

**A read whose *purpose* is verifying a write must be remote.** `item children` defaults to local like every other read, but its zero answer is the one a caller acts on — and the local mirror cannot tell a childless parent from one whose children the `zot` binary wrote seconds ago. So `--remote` exists, and the zero-children case carries a `stale-local` warning saying the mirror cannot distinguish the two. `Md5` rides on both `local.ChildItem` (from `itemAttachments.storageHash`) and `api.ChildItem`, so the two listings agree field for field and which plane answered never changes the payload shape.

**Adding new operations:** reads go on `local.Reader` (and `*local.DB`). API-side read methods belong on `*Client` directly (like `GetItem`, `ListCollections`, `ListGroups`) and stay out of any interface — the `--remote` flows are the only callers and they're explicit at the call site. **A new WRITE verb does not belong here at all**; it belongs in the `zot` binary.

## Library scope (personal vs. shared)

Every zot command except `setup`, `import`, and `guide` runs under a `--library personal|shared` scope. The flag is a persistent root-level flag (`cli.PersistentFlags()`) validated by `cli.ValidateLibraryBefore` — which deliberately does **not** require it: when the flag is absent, `ensureLibraryScope` auto-selects personal when it's the only configured library, prompts interactively when both are configured, and hard-errors only under `--json` / non-TTY. The resolved `zot.LibraryRef` is stashed on ctx; `info` optionally takes `--library` to narrow, and with no flag it summarizes every library the account has access to.

**`--library all` (merged read pool)** passes flag validation everywhere but is **gated per command** — `search` and `bib` accept it (via `openLocalDBAllowAll`), and `browse` opens `local.Open` directly and *defaults* to `all` whenever a shared group is configured; everything else rejects it with the single-library rewrite, because their query paths still bind `d.libraryID` and would silently answer personal-only. The end state is `all` working wherever `--library` is accepted; convert a command's queries through `DB.libIn` before opting it in. Mechanics: `local.ForAll` resolves personal + the configured group into `DB.libraryIDs`; the converted paths (`SearchWithTotal`, `listWhere` → List/ListAll/CountList, `Read`, `ResolvePDFAttachment`) filter through `libIn`, and every local read stamps `Item.Library` ("personal"/"shared" — zen renders it per row; constant on single-library calls). Under `all`: `LibraryID()` returns 0 (top-level `library_id` is meaningless, per-row provenance rules), `requireAPIClient` errors (no merged API path — `Resolve(LibAll)` leaves `APIPath` empty on purpose), and `search` conflicts `--remote`/`--notes` (no merged endpoint / unconverted filter — an honest error beats a silently narrower answer). `bib` resolves against the merged `ListAll` pool; cross-library duplicates hit the existing >1-distinct-matches ambiguity gate, never a guess.

- **Config** carries both libraries: `UserID` (personal) and `SharedGroupID` + `SharedGroupName` (shared group). Setup auto-detects the shared group via `/users/{userID}/groups` when the account belongs to exactly one; it errors with options listed when the account belongs to ≥2.
- **Resolution** — `Config.Resolve(scope)` maps a scope to a full `LibraryRef{Scope, APIPath, LocalID, Name}`. `Config.ResolveWithProbe(scope, probe)` lazy-detects the shared group on first use if `SharedGroupID` is blank and persists the result.
- **API dispatch** — `api.Client.Lib` drives the switch between `c.Gen.{Op}WithResponse(ctx, UserID, …)` and `c.Gen.{Op}GroupWithResponse(ctx, GroupID, …)`. Generated from the OpenAPI spec + the `scripts/zotero-mirror-paths.yq` transform, so every `/users/{userID}/…` path has a parallel `/groups/{groupID}/…` twin (except `/users/{userID}/groups` itself). `api.New(cfg, api.WithLibrary(ref))` is required — no default; passing nothing errors.
- **Local dispatch** — `local.Open(dir, sel)` accepts a selector: `ForPersonal()` for `type='user'`, `ForGroup(libraryID)` for a specific SQLite libraryID, or `ForGroupByAPIID(groupID)` when you only know the Web API group ID (joins the `groups` table to resolve).
- **CLI plumbing** — `openLocalDB(ctx)` and `requireAPIClient(ctx)` both read the scope from ctx and wire it through. A missing ref in ctx is a hard error — it means the command was registered outside the Before hook.

## `zot browse` — inline search-and-open REPL

Deliberately a REPL, not a TUI: an `x/term` readline loop (history + emacs editing) printing results into scrollback, with raw mode confined to the ReadLine wrapper so a panic can't strand the terminal (`cli/repl.go`). Grammar: bare text searches via the normal DSL + ranking; a bare number opens that hit's PDF (`ResolvePDFAttachment` → `AttachmentPath` → `LaunchFile`, every failure a non-fatal message); `:library` switches scope; `:limit` pages; `:h`/`:q`. Scope defaults to `all` (personal when no shared group is configured). Interactive-only — `--json` and non-TTY are rejected with a `zot search` rewrite. Every side effect is injected so tests script whole sessions.

## Generated client (`client/`)

`internal/zot/client` is generated from the Zotero OpenAPI spec via `just zot-gen`. Provenance, the user→group path-mirroring transform, and the add-an-endpoint workflow are documented in `client/doc.go`. **Never hand-edit `zotero.gen.go`** — needed surface goes through `internal/zot/api`.

## Hygiene checks

**`doctor` is read-only reporting, and that is the boundary — not a default.** Every check reads `zotero.sqlite` and stops: no Zotero write, no network, no metered lookup. The repairs the reports point at live in the sibling `zot` binary, which owns the credential — `zot fix dois`, `zot fix citekeys`, `zot enrich`, `zot doctor pdfs`. Two tools answering one question is what the three-tool split forbids, so the write arms were REMOVED (2026-08-12), not deprecated: `dois --fix/--apply`, `missing --enrich/--apply`, `citekeys --fix/--apply/--kind/--item` are gone and now fail as unknown flags, which is the point — a script still passing one must not look like it worked.

Six sub-commands live under `zot doctor` (`invalid`, `missing`, `orphans`, `duplicates`, `citekeys`, `dois`), plus the `pdfs` stub; bare `zot doctor` runs the aggregate over `DoctorChecks` (`invalid` → `missing` → `orphans` → `duplicates` → `citekeys`) — `dois` runs only when named. SQL in `local/hygiene.go` + `local/orphans.go`; pure logic (validators, clusterers) in `hygiene/` so they're unit-testable without a DB. Every check returns `*hygiene.Report{Check, Scanned, Findings, Clusters, Stats}`; `Stats` is per-check and read by renderers via type assertion.

**Severity taxonomy** (consistent across checks):

- `SevError` — structurally broken (missing title, attachment file gone from disk)
- `SevWarn` — citation-affecting (missing creators/date, malformed DOI/URL/date, standalone attachment)
- `SevInfo` — coverage gaps and user-workflow choices

**Doctor ordering:** cheap/structural first (see `DoctorChecks`). `--deep` enables fuzzy duplicates + `uncollected-item` orphan kind. `--check-files` stays a per-command opt-in (stat's every attachment).

**Opt-in sub-checks** (in `AllOrphanKinds`, not `defaultOrphanKinds`): `orphans --kind uncollected-item`, `orphans --kind missing-file --check-files`.

**Duplicate PDF content is detected here, not in an extraction planner.** `cli/duplicates_content.go` hashes each item's PDF (`extract.HashPDF`) and groups on the mtime-free projection (`extract.ContentKey`; the full fingerprint embeds mtime and misses separate downloads of the same file). It is a warning, never an automatic merge.

## `zot notes` — the notes YOU wrote

The noun split is load-bearing: **an extraction is the paper, not a note.** Live counts — personal 4,719 notes of which 4,710 are docling, group 421 of which 388 — so an unfiltered `notes list` is a listing of extractions with 9 real notes lost inside it.

- `local.ListNotes` is the inverse of `local.ListAllDoclingNotes` (`NOT EXISTS` on the docling tag); the two partition the library's notes. It **LEFT-joins the parent** where the docling query inner-joins — standalone notes have no parent and are exactly the ones worth surfacing.
- The extraction side of that split (`zot content`, `zot extract`, `zot extract-lib`, `zot llm`) left sci entirely: producing an extraction runs docling and posts a child note through the Web API, which is a credentialed write, and reading one is now `zot read` over a corpus that reports its own coverage. `notes add|update|delete` stay registered as stubs whose `Try` carries both moves, because a user typing `zot notes add` today is two renames behind.
- `internal/zot/extract` survives as `HashPDF` / `ContentKey` for `doctor duplicates`, and nothing else — the two tag constants left with `link suggest` (2026-08-13), the last verb that asked whether a note was an extraction. The `docling` tag now lives only where it is queried, in `pkg/local`'s SQL, and the parent-side tag is `local.HasMarkdownTag` (what `search --notes` filters on). Nothing in the package runs docling.
- `notemd.StripProvenance` drops the YAML provenance block an extraction note opens with. Detection is positive-evidence gated (leading `---` fence + `zotero_key:` within `maxProvenanceLines`), same discipline as `notemd.IsHTMLNote` — guessing wrong eats the top of a paper. It is what keeps `noteSnippet` showing prose instead of `--- zotero_key: … title: "…" source…`.

## `zot export --format ndjson` — the item-plane mirror (`dump.go`)

A **mirror, not a bibliography**. The citation formats project the library down to what a `.bib` needs; ndjson keeps every field a downstream store might join on (versions, collection membership, tags, attachment metadata) and leaves interpretation to the reader.

The `zot` binary grew its own ndjson dump for its pipeline, and that duplication is accepted: sci's export serves any user's library, zot's serves its own staging. **A field rename here is still a breaking change for anything reading these records.**

- **Kind-tagged records, collections before items.** One JSON object per line, each carrying `kind` (`collection`/`item`) and `library` flattened into the payload. Collections lead so a streaming consumer can resolve an item's collection keys as it goes instead of buffering the file.
- **ndjson is the one format that opts into `--library all`** (via `openLocalDBAllowAll`). Its consumer wants the whole corpus in one file, and both `ListAll` and `ListCollections` are converted through `libIn`. The citation formats stay single-library: merging both libraries into one `.bib` would emit the deliberate cross-library duplicates as separate entries. Under `all` the top-level scope is meaningless, so **every record carries its own `library`** — that per-row provenance is the only correct answer, and a test pins it.
- **`ListCollections` was converted for this** (`local/collections.go`): it bound `libraryID = ?` and would have silently answered personal-only under `all`. `Collection.Library` is stamped the same way `Item.Library` is. This is the documented "convert a command's queries through `DB.libIn` before opting it in" path.
- **The `.meta.json` sidecar is written LAST and carries the body's sha256**, so its presence is the completeness signal — a consumer finding a body with no matching sidecar knows it caught a partial write. It also carries `last_sync` and `pending_wal_bytes`, which is how the WAL blind spot (immutable mode can't see uncheckpointed commits) reaches the downstream store instead of dying here. Without `--out` there is no sidecar and the human output says so.
- **Cite-keys are enriched before serialization** (`citekey.Enrich`). Stored Zotero 7 `citationKey` alone leaves most rows blank; the BBT `Citation Key:` line in Extra and the synthesized fallback are what the consumer's citekey column actually needs.
- **No local filesystem paths.** Attachment paths are mbp-local and resolve to nothing on the machines that read the dump, so a path in the mirror is a broken promise. Pinned by a test.
- **`search --export --format ndjson` is refused** with a `Fix` naming `sci zot export --format ndjson`: a dump of arbitrary search hits is not a coherent item plane.

## Relations (`zot link list`) — reads only

Zotero's "related items" (`dc:relation`) connect a standalone note to the papers it discusses. **sci reads them and stops there since 2026-08-13**; `link add`, `link rm` and `link suggest` are stubs pointing at the zot binary. Reads split the predicates by owner: `Related` is the user's own `dc:relation`, `Other` is Zotero's bookkeeping (`owl:sameAs`, `dc:replaces`), which sci has never written. `api.patchRelations` survives in the client with its whole-object PATCH rule intact (see its godoc) — unreachable from the CLI, and rule 18's third half is what keeps it that way.

- **Relations live on `local.Item`, snippets don't** — and the difference is *query-derived vs intrinsic*. A search snippet depends on the query, so it rides beside the item in `ListResult.Snippets`. Relations are a property of the item, like `Tags`/`Collections`/`Attachments`, which are themselves only populated by `Read` / `api.ItemFromClient` and left empty by `List`/`Search`. This matters because `ItemResult.JSON()` returns the **bare `local.Item`**: anything hung on the result shell instead of the item is invisible under `--json`. The field is a pointer with `omitempty`, so an item with no relations emits a byte-identical shape to before the field existed.
- **One labelling mechanism: `local.ItemLabels`** (one query, not N). Both ends of a relation may be an item or a note, and the two store their name differently; `ItemLabels` returns whichever applies and omits keys with no local row (a relation into another library is normal, not an error). `Read` populates `ItemRelationSet.Titles` from it, and `link list` reads titles off the same field rather than carrying its own map.
- **`item read` without `--remote` will not show a link written seconds ago** — the local mirror lags until Zotero desktop syncs. That's the general reads-local lag, covered by `staleLocalWarning`; the `item read` guide entry names it explicitly because relations are the case where it bites hardest. `link list --remote` exists for exactly the same reason, and the relation `zot link add` just wrote is the case that made it non-optional: measured live, a stale mirror read "10 proposed" where the API read "10 already-linked" — same ten papers.
- **The note-scanning `suggest` is gone and is not coming back.** It read one note, resolved every reference in its body, and proposed a relation per hit. zot's `link suggest` answers a different question (work-identity twins from its corpus), and promoting `bib` so zot could ask sci's was declined on 2026-08-13. What survives of it here is `bib` itself, still live under `sci zot bib`: `bib.KindZoteroKey` is why `zotero://` gets its own scanning pass (`bib.ScanText`'s `urlRe` is anchored to `https?://`), and `bib.ResolveRefs` is `Resolve` with the per-reference mapping kept.

## `zot import` — Zotero desktop connector (`internal/zot/connector/`)

`zot import <path>` drag-drops a PDF into the running Zotero desktop via its local connector server (the metadata-recognition pipeline runs too). **The one sanctioned write path, and the user's own app makes it** — sci hands over a file and Zotero does the rest, including the sync. Exempt from `--library` (desktop writes to whichever library its UI has selected). The undocumented wire-format landmines (Content-Length vs chunked, the 204 "no match", the non-Mozilla UA that dodges the CSRF guard, the stripped item key) are documented on the `internal/zot/connector` package + `client.go` godoc — read that before touching it. `connector/client_test.go` is the regression line if desktop's response shape changes.

## Conventions

- **Raw `database/sql` in `local/`** — same exception family as `dbtui`. Local reads are perf-sensitive and don't need dbx ergonomics.
- **All inputs validated at the command layer.** `internal/zot.Setup()` expects pre-validated args; interactive prompting and `--json` non-interactive validation both live in `cli/setup.go`.
- **Every command that reaches the Web API short-circuits via `requireAPIClient(ctx)`** — reads the library scope from ctx, checks `RequireConfig()` + `netutil.Online()`, and builds the API client with `WithLibrary(ref)`. Destructive ops go through `cmdutil.ConfirmOrSkip` with `--yes` bypass.
- **Library scope is resolved before every scoped command runs** (see "Library scope" above) — the persistent flag is wired into both entry points via `cli.PersistentFlags()` + `cli.ValidateLibraryBefore`. Setup configures both libraries at once; info summarizes both when the flag is absent.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--user-id` when `--json` is set. Any new prompting command must do the same check.

## Saved searches (`/searches`) — reads only, and that is the API's fault

Zotero's saved searches are a parallel surface to collections: named virtual queries with `{condition, operator, value}` triples. sci exposes `zot saved-search {list,show}`; **the writes retired outright (2026-08-12) with no home in either binary.**

**The Web API can store a saved search's definition but never evaluates it.** Only the desktop client runs the query. So a search created from a CLI is a real row that returns nothing to anybody — and that is not hypothetical: a pipeline predicate built on a saved search silently no-op'd for exactly this reason. Writing one is Zotero desktop's job, and the stubs say so in prose rather than pointing at `zot`.

The reads stay because reading a definition back is still useful, and both are **live-only** — a saved search has no row in the local mirror worth reading.

- **Name resolution lives on `api.Client.ResolveSavedSearch`** (key-or-name, name matching case-insensitive like collections). Names must never reach `GET /searches/{key}`: the live Zotero API answers a name-shaped key there with a bare **500**, not a 404 — resolution goes through `ListSavedSearches`. That 500 is also what exposed the retry-middleware bug (see Gotchas).
- The write-side quirks are gone from sci but were real, and are worth knowing if the verbs are ever reconsidered: update was a full replacement (no single-search PATCH endpoint), and `joinMode` / `noChildren` / `includeParentsAndChildren` are *pseudo*-conditions that modify a search rather than filter it, stored as leading entries in the same array. `internal/zot/api` still carries `CreateSavedSearch` / `UpdateSavedSearch` / `DeleteSavedSearch`; nothing in the CLI calls them.

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
the bug AND exercises every production slice flag — which is now just
`doctor --check` — to prevent regressions. If the reproduction test ever
starts passing with `Local: true`, urfave/cli has fixed the upstream bug
and the waivers can be removed.

**Rule 17 is the gate.** Rule 4 requires `Local: true` on every flag, which
on a slice flag is precisely the bug above — so the waiver comment was the
only thing preventing it, opt-in by memory. `zot doctor --check` shipped
with `Local: true`, and `--check invalid --check missing` silently ran ONLY
`missing` while its report looked complete. Rule 17 now rejects
`Local: true` on any `*SliceFlag`, so the waiver can't be forgotten.

**Orthogonal gotcha — the comma split.** urfave/cli's default slice
separator is `,`, so a free-text value arrives in pieces. `--author
"Smith, Alice"` became `["Smith", " Alice"]`, and `parseCreator` read each
comma-less half as an INSTITUTIONAL name — one author silently became two
organizations. `--condition "title:contains:Cambridge, MA"` split into a
valid spec plus the unparseable fragment `" MA"`, an error naming something
the user never typed. The cure is `DisableSliceFlagSeparator`, a
*command*-level setting with no per-flag form and no inheritance from the
root, so each command opts in on its own.

**No surviving command opts in, because none carries a free-text slice
flag any more** — the last two (`item add --author/--creator/--field`,
`saved-search {create,update} --condition`) left with their write verbs.
`doctor --check` is deliberately NOT opted in: its values are enum names,
and it parses the comma form itself on purpose.

`TestSliceFlagSeparator_Reproduction` therefore pins the behaviour
synthetically, in both directions. Keeping it is the point — the trap costs
silent data corruption, and the next person to add a free-text slice flag
needs it written down and gated rather than rediscovered on a live library.
Turn it back into a production table the moment such a flag lands.

## Gotchas

- **`zot guide --json` is a machine contract**: `data.contract_version` (currently **2**) is pinned exactly by `guide_test.go`. v2 is the shrink to the local read surface — `find`, `graph`, `content`, `llm`, the item and collection writes and `search --content` all left the catalog, so a consumer holding v1 holds a map to verbs that now refuse. Bump it whenever the guide's JSON shape or catalog changes meaningfully; additive changes don't.
- **`ListAll` is lossless, so every BIBLIOGRAPHY surface must filter it itself.** `ListAll` forces `ListFilter.Mirror`, because it is what the NDJSON item-plane mirror is built from — standalone attachments, standalone notes and annotations are real Zotero items and dropping them would put a hole in the mirror. They are not references, so `ExportLibrary` filters them back out through `local.IsBibliographic` before either citation format sees them. Measured live: 36 non-references in the personal `.bib` and 124 in the shared one, and because an annotation has no title they were exactly the file's 36 titleless `@misc` entries — an entry nothing consuming the `.bib` can render or verify, inflating "how many references do I have" from the file that is supposed to be authoritative. The rule is stated twice on purpose — `hygieneItemTypeFilter` for queries that filter at the source, `local.IsBibliographic` for callers handed rows — and `TestNonBibliographicTypesMatchSQL` is what stops the two drifting. `ExportStats.Skipped` reports the drop so it is never silent; `zot bib` and `search --export` inherit all of it through `runLibraryExport`.
- **BibLaTeX export escapes for COMPILATION, not typography** (`export.go`). A bare `&` is a tabular alignment character, `%` comments away the rest of the line, and `# $ _` are equally active — the live library carried 180 such values, 126 of them journal names, so any manuscript citing one failed to build with the error pointing at the `.bib`. `bibEscape` covers `\ { } & % $ # _ ~ ^` in one Replacer pass (a sequence of ReplaceAll would re-escape the braces the earlier replacements introduce). It previously mapped `\` to `\\`, which is a LINE BREAK in LaTeX, not a literal backslash. **`url` and `doi` go through `writeBibVerbatimField` instead** — biblatex declares them verbatim, so escaping an underscore corrupts the address rather than protecting it; only braces are touched there, because a raw brace ends the value early and desynchronises every entry after it.
- **A well-formed DOI prefix is not a well-formed DOI.** `ValidateDOI`'s suffix pattern accepts almost any character on purpose (Kluwer's `10.1023/a:NNN`, Wiley's SICI angle brackets), so a whole second DOI pasted inside one passed every check: `10.1145/http://dx.doi.org/10.1145/2600057.2602892` sat in the live library reading as valid. It is not cosmetic — a DOI is the tier-1 identity key, so a nested one resolves nowhere and reads downstream as "the index doesn't have this paper" rather than as a typo.
- **The retry middleware must return a readable body on its last 5xx attempt** (`api/retry.go`): retryable responses are drained+closed between attempts (keep-alive reuse), but the final one is handed to the generated client, whose `ReadAll` on a closed body fails with "file already closed" — masking the real HTTP status. Bitten live via Zotero's 500-on-name-shaped-`/searches/{key}` answer. Pinned by `TestRetry_5xxExhausted_SurfacesStatusNotClosedBody`.
- **`item read` is a batch verb**: it takes multiple keys, and `--missing-ok` turns per-key not-founds into a `{count, items, missing}` report instead of failing the whole batch. The `missing` bucket is fed only by `api.ErrNotFound` — transport failures still error, so an outage can't launder into "item doesn't exist".
- **Zotero date storage**: `itemDataValues.value` for the `date` field is `"YYYY-MM-DD originalText"` — first token sortable, second is user input. `cleanDate()` strips after the first whitespace for display. Keep raw values in JSON output so downstream tools see authentic data.
- **Zotero date `00` padding**: the sortable form pads unspecified components with `00`, not by truncating. Year-only is `"1871-00-00 1871"`, not `"1871 1871"`. `ValidateDate` treats `month=0`/`day=0` as "unspecified" markers — caught by the real-library smoke test after the first TDD pass flagged 4995 false positives.
- **Schema version drift**: `SchemaOutOfRange()` warns if `version.userdata` is outside `[MinTestedSchemaVersion, MaxTestedSchemaVersion]`. Current tested 120–130 (live DB is 125 as of 2026-04-11). Widen only after verifying every query in `items.go` / `collections.go` / `tags.go`.
- **`tagFilter` vs `tag`**: `DeleteTagsParams.Tag` is a pipe-separated string (`"a || b || c"`), NOT a slice. API caps 50 tags per request — see `DeleteTagsFromLibrary`'s batching.
- **Bib export is BibLaTeX** (`export.go`, `exportlib.go`, `citekey/`): every `.bib` surface (`export`, `item export`, `search --export`, `bib`) emits BibLaTeX vocabulary (`journaltitle`, ISO `date`, `@thesis`/`@report`/`@incollection`/`@online`), matching Zotero's built-in BibLaTeX translator so sci entries and SciCite/vscode-zotero entries coexist in one file; `--format bibtex` is accepted as an alias (`ExportFormat.Canon`) and emits the same output. Cite-key policy split into `citekey` sub-package to break the `zot → hygiene` import cycle. **Breaking change warning:** `citekey/citekey.go` constants (`wordCount`, `wordMaxLen`, stopword list) define the synthesized key format `{author}{year}-{words}-{ZOTKEY}` — changing any rewrites every key. Drift detection: `.zotero-citekeymap.json` sidecar emits `ids = {oldkey}` aliases.
- **Cite-key repair left with the rest of the write plane.** `zot doctor citekeys` reports; `zot fix citekeys` in the zot binary rewrites. Rewriting keys is destructive against a BBT-managed library, which is exactly why the verb belongs where the credential and the confirmation live.
- **Publisher-subobject DOI patterns live in `pkg/doi/`** (Frontiers `/abstract`+`/full`, PLOS `.tNNN`/`.gNNN`/`.sNNN`, PNAS `/-/DCSupplemental*`, eLife assets, PeerJ subobjects). They back `zot doctor dois` here — which reports and stops — and the repair that rewrites the stored DOI in the zot binary. `pkg/` is the shared surface, so that catalog is defined once and imported by both.
- **`ZOT_REAL_DB` env var** / **`./zotero.sqlite`**: `local/realdb_test.go` uses `ZOT_REAL_DB`. Hygiene real-library tests open `./zotero.sqlite` at the repo root (gitignored) and gate behind `SLOW=1`. Never hardcode the user's live library path.
- **Single-name creators**: institutional authors like "NASA" are stored with `fieldMode=1` and the name in `lastName`. `Creator.Name` carries these; `Creator.First`/`Last` stay empty. Bib export emits them as `{NASA}` to suppress name parsing.
- **Shared fixture**: `local/fixture_test.go` builds the synthetic `zotero.sqlite` once per `go test` invocation (`sync.Once` + `TestMain`). Adding tables/rows may require updating `TestStats` and `TestListCollections` counts. The fixture includes both a user library (`libraryID=1`) and a group library (`libraryID=2, groupID=6506098, name='sciminds'`) so both scopes exercise real rows.
- **Library IDs**: two numbering systems are in play. The **Zotero Web API group ID** (e.g. `6506098`) is what `zot.Config.SharedGroupID` and every `/groups/{groupID}/…` URL carries. The **SQLite `libraries.libraryID`** (e.g. `2` for group, `1` for user) is what `local.*` queries filter on. `local.ForGroupByAPIID` bridges the two via a join on the `groups` table.
- **Config schema migration**: `LoadConfig` silently rewrites pre-rename `library_id` → `user_id`. When adding a new field rename, extend `migrateLegacyConfig` in `config.go` with the new mapping and add a test in `config_test.go` that seeds the legacy shape. Without this, users of the old schema get a misleading "zot not configured" error even though the file exists.
- **`zot search` ranking & scope**: hits are fetched without a SQL `LIMIT` and ranked in Go (`rankSearchResults` in `local/items.go`) by title relevance (count of positive query words in the title), then year desc; the `--limit` slice happens **after** ranking so a broad query can't crowd out the top hit. The base CTE now also lifts `citationKey`, so bare terms and the `@citekey:`/`@key:` field match the stored key **and** the 8-char Zotero item key (a pasted whole synthesized key resolves via its `-ZOTKEY` suffix, `synthKeySuffixRe`). Local-only — mutually exclusive with `--remote`. `--full` hydrates hits into full items instead of the brief rows. `--notes` FILTERS to items that have an extraction; it does not search inside anything. `local.SearchFulltext` still exists but is no longer on this path: its only caller is dbtui via `view/store.go`.
- **Free-text expansion** (`expandFreeText` in `local/items.go` — zot-side, post-parse; dbtui's shared match parser is untouched): each positive free-text clause splits into per-word AND clauses (every word must match *some* field), a bare 1500–2099 token becomes an `@year:` clause (quote it — `"2021"` — to match it literally; it no longer matches DOI substrings, by design), and a quoted phrase stays one clause with the outer quotes stripped for the metadata needle. Negated free text stays unsplit (splitting would De Morgan it); smartcase applies per word.
- **`zot search --export`** shares `runLibraryExport` with `zot export` and honors `--format biblatex|csl-json`; hydration is one batched `ListAll{Keys}` call (not per-hit `Read`).
- **`zot bib <file-or-dir>`** (`internal/zot/bib/` + `cli/bib.go`): scans markdown/Quarto (`.md`/`.markdown`/`.qmd`) for pandoc `@citekeys`, `[[wikilinks]]` (embeds `![[…]]` skipped), DOIs, arXiv ids, and URLs → resolves against the local library → emits a bibliography of exactly the cited items via the same export pipeline (so cite-keys, the `.zotero-citekeymap.json` sidecar, and drift aliases all apply). **Resolution never guesses**: a ref matching >1 distinct item is reported unresolved with the candidate count; unresolved refs always surface (honesty gate) in `BibResult.Unresolved` and the human footer. `bib` package is pure (no I/O) — `ScanText` + `Resolve`; the CLI owns file walking (`--recursive` skips hidden dirs) and DB access. Cite-key lookup uses `citekey.Resolve`, so a key from `zot export` round-trips even after its synthesized prefix drifts (matched via the `-ZOTKEY` suffix).
- **`zot bib --verify` retired (2026-08-12), and the unresolved list is bib's whole answer.** Verification asked OpenAlex and doi.org whether an unresolved ref is a real work — an upstream lookup, network and metered, on a surface that otherwise reads only `zotero.sqlite`. It was reimplemented zot-side; `bib/verify.go`, its `cli/bib.go` adapters and `BibResult.Verified` are gone. Two things it taught are worth keeping: **the lookup chain was load-bearing** — OpenAlex alone produced false accusations on the first real-manuscript run, 404ing on monographs (Carey's OSO book) and unindexed preprints while `not-found` renders as "likely fabricated", so only a registry 404 justified that verdict — and **a transport error must never collapse into `not-found`**, which is the same rule as "an empty result is a gap, not a claim about the literature".
  - What survives here is the honest half: `Unresolved` always surfaces, carrying `Reason` and — on an ambiguity — the competing Zotero keys. It is a statement about THIS library and says nothing about whether the work exists.
  - `bib.trimRefTail` balance-trims a trailing `)` (prose `(doi:10.x/y)` vs. Wiley SICI `10.1002/(SICI)…(19980815)`). Unbalanced parens made real DOIs 404 — that mattered most under verification, but a mangled DOI is a wrong `Unresolved.Reason` regardless.

## Reading notes back out (`notemd.HTMLToMarkdown`)

`notemd` is bidirectional. Without the reverse direction Zotero would be a **write-only store** — a literature note could be posted and never cleanly recovered, which blocks every downstream consumer (an agent re-reading its own note, an external index, a bibliography pass). `--md` on `zot notes read` and `zot item note read` adds a `markdown` field; it is **opt-in** so the default `--json` shape stays byte-stable for existing agents (and a 485KB note doesn't double). Human output on both renders markdown instead of the old tag-stripper, which used to flatten every heading, list marker and link.

**`IsHTMLNote` is the load-bearing part — don't simplify it to "convert everything".** In the live library **5,098 of 5,140 notes are raw markdown inside Zotero's wrapper div**, not HTML: the docling extraction path posts markdown by default, and Zotero just wraps whatever it's given. Running those through an HTML→markdown converter is destructive — HTML collapses whitespace, so the YAML provenance block at the top of every extraction note becomes one unparseable line, and `zotero_key` comes back `zotero\_key`.

Detection defaults to HTML (that's what Zotero declares the field to be) and escapes to markdown only on positive evidence, in order:

1. A block-level tag **opening** the body → HTML. Testing what opens it, not what appears anywhere, is deliberate: docling markdown embeds HTML tables mid-document and is still markdown.
2. Otherwise markdown only if a markdown block construct is present (frontmatter fence, ATX heading, list, code fence). Without this a body of inline-only HTML (`<b>bold</b> and <i>italic</i>`) has no leading block tag and would pass through as raw tags.

Round trip is pinned over exactly the sanitizer's tag vocabulary, plus `zotero://select/...` URIs (Zotero's item-link form — losing those would silently break note→item links). Converter is `html-to-markdown/v2` with base+commonmark only, horizontal rule configured to `---` to match what `MarkdownToHTML` round-trips from.

Note the two `local` helpers that predate this and still have their own jobs: `local.UnwrapZoteroDiv` (wrapper strip) and `local.NoteText` (`notetext.go` — tags→text, the *indexing* path, not display).
