# CLAUDE.md — zot (internal/zot/)

Zotero library management. Also installable standalone via `cmd/zot/` (mirrors the `dbtui` / `markdb` pattern).

## Architecture

**Two surfaces, one command tree.** The full urfave/cli v3 tree lives in `internal/zot/cli.Commands()`. Both entry points import it:

- `cmd/zot/main.go` — standalone `zot` binary
- `cmd/sci/zot.go` — `sci zot …` subcommand

Any subcommand added to `internal/zot/cli` shows up in both surfaces automatically. Do not duplicate wiring.

**Reads local, writes cloud.** This split is load-bearing:

- **Reads** → `internal/zot/local` opens `zotero.sqlite` with `file:…?mode=ro&immutable=1&_pragma=query_only(1)`. Immutable mode skips WAL processing entirely so we never contend with the running Zotero desktop app's locks.
- **Writes** → `internal/zot/api` calls the Zotero Web API. We do NOT wait for local sync-back — Zotero desktop listens on its own sync stream and updates `zotero.sqlite` automatically. API success = done, from our side.

Corollary: immediately after a write, the local DB will briefly diverge from the server. That's fine. Don't add polling or consistency checks.

## Package map

```
internal/zot/
├── config.go / setup.go / *_test.go      # XDG config + Setup() / Logout() business logic
├── result.go / readresult.go / writeresult.go  # cmdutil.Result types for read/write commands
├── hygieneresult.go                      # Result types for hygiene commands (Missing/Duplicates/Invalid/Orphans)
├── doctor.go / doctor_test.go            # Doctor() orchestrator + DoctorResult renderer
├── export.go / export_test.go            # CSL-JSON + minimal BibTeX emitters
├── open.go                               # attachment path resolution + LaunchFile
├── cli/                                  # SHARED command tree
│   ├── cli.go                            # Commands() factory
│   ├── setup.go                          # setup command (huh form + flags)
│   ├── read.go                           # search/read/list/stats/export/open
│   ├── write.go                          # add/update/delete/collection/tag
│   ├── hygiene.go                        # missing/duplicates/invalid/orphans commands
│   └── doctor.go                         # doctor umbrella command
├── client/                               # GENERATED — do not hand-edit
│   ├── zotero.gen.go                     # `just zot-gen` regenerates from webapps/apis/zotero/openapi.yaml
│   ├── config.yaml                       # oapi-codegen config
│   └── doc.go
├── api/                                  # Generated-client wrapper
│   ├── client.go                         # New(cfg, opts…) with auth/backoff/412 retry
│   ├── retry.go                          # 429/5xx middleware (NOT 412 — that's per-op)
│   ├── keys.go                           # CurrentKey()
│   ├── items.go                          # Create/Update/Trash + withVersionRetry
│   ├── collections.go                    # Create/Delete
│   ├── tags.go                           # DeleteTagsFromLibrary
│   └── *_test.go                         # httptest-driven, includes a fake Zotero server
├── hygiene/                              # Library-quality checks (pure + DB-backed)
│   ├── hygiene.go                        # Severity / Finding / Cluster / Report taxonomy
│   ├── missing.go / missing_test.go      # field-presence coverage check
│   ├── duplicates.go / duplicates_test.go  # DOI + title clusterer (pure)
│   ├── invalid.go / invalid_test.go      # DOI/ISBN/URL/date validators
│   ├── orphans.go / orphans_test.go      # 6 structural-dangling sub-kinds
│   ├── normalize.go / normalize_test.go  # title normalization (shared with duplicates)
│   └── similarity.go / similarity_test.go  # Levenshtein + SimilarityRatio + capped variant
└── local/                                # Read-only sqlite (raw database/sql)
    ├── db.go                             # Open() + schema version probe
    ├── types.go / items.go / collections.go / tags.go
    ├── hygiene.go                        # ScanFieldPresence, ScanDuplicateCandidates, ScanFieldValues
    ├── orphans.go                        # ScanEmptyCollections, ScanStandalone*, ScanUncollected*, ScanUnusedTags, ScanAttachmentFiles
    ├── fixture_test.go                   # synthetic zotero.sqlite (sync.Once shared across tests)
    └── realdb_test.go                    # opt-in smoke via ZOT_REAL_DB env var
```

## Conventions

- **Raw `database/sql` in `local/`** — documented exception to sci-go's pocketbase/dbx rule, alongside `internal/tui/dbtui/data` and `internal/markdb`. Local reads are performance-sensitive and don't need the dbx ergonomics.
- **All inputs validated at the command layer.** `internal/zot.Setup()` expects pre-validated args; interactive prompting and non-interactive flag validation both live in `internal/zot/cli/setup.go`.
- **Every write command short-circuits** via `requireAPIClient()`: checks `RequireConfig()` + `netutil.Online()` before building the API client. Destructive ops (`delete`, `collection delete`, `tag delete`) go through `cmdutil.ConfirmOrSkip` with a `--yes` bypass.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--library` when `--json` is set. Any new command that prompts must do the same check.
- **No inline lipgloss.** Use `ui.TUI.*()` everywhere, same as the rest of sci-go.

## Generated client (`client/`)

Regenerate with `just zot-gen`. The recipe:

1. Reads `/Users/esh/Documents/webapps/apis/zotero/openapi.yaml` (OpenAPI 3.1).
2. Rewrites it to 3.0 on the fly because oapi-codegen v2 does not yet support 3.1:
   - `openapi: 3.1.0` → `3.0.3`
   - `type: [string, "null"]` → `type: string\n  nullable: true` (and same for integer)
   - Renames the `parameters.tag` component to `tagFilter` to avoid a Go-type collision with `schemas.Tag`
3. Runs `oapi-codegen -config internal/zot/client/config.yaml` against the preprocessed temp file.
4. gofmts the output.

If the upstream spec grows new 3.1-isms or new name collisions, extend the `sd` pipeline in the justfile rather than hand-editing the spec or the generated file.

**Never hand-edit `zotero.gen.go`.** Any needed surface goes through `internal/zot/api`.

## 412 Precondition Failed pattern

Zotero uses optimistic concurrency: writes include `If-Unmodified-Since-Version` (header) or `version` (body field), and the API returns 412 if the target has advanced.

**Why it's not in the middleware:** recovering from a 412 requires re-reading the object to get the fresh version AND rebuilding the request payload — the retry middleware (`internal/zot/api/retry.go`) only knows about HTTP, not about object semantics. So 412 recovery is per-operation.

**The helper:** `withVersionRetry(fn, getVersion, initial)` in `items.go`. `fn(ver)` performs the write; on `*VersionConflictError`, `getVersion()` refreshes and `fn` is called once more. More than one retry would indicate a hot-contention loop and we'd rather surface it.

Every write operation that uses this helper owns its own `getVersion` closure so the refresh path is explicit at the call site.

## Hygiene checks (`hygiene/` + scans in `local/`)

Read-only library-quality checks, each fronted by its own `zot <check>` subcommand. A future `zot doctor` will run all four and merge reports.

**The shape.** Every check returns `*hygiene.Report`:

```
Report {
  Check    string         // "missing" | "duplicates" | "invalid" | "orphans"
  Scanned  int
  Findings []Finding      // per-item issues (most checks)
  Clusters []Cluster      // grouped issues (duplicates only)
  Stats    any            // per-check summary blob — typed-asserted by renderer
}
```

Findings and Clusters are mutually informative, not exclusive. `Stats` is a check-specific struct (`MissingStats`, `DuplicatesStats`, `InvalidStats`, `OrphansStats`) that renderers read via type assertion.

**Severity taxonomy.** Graded consistently across checks:

- `SevError` — structurally broken (missing title, attachment file gone from disk)
- `SevWarn` — citation-affecting (missing creators/date, malformed DOI/URL/date, standalone attachment)
- `SevInfo` — coverage gaps and user-workflow choices

**The pure/DB split.** SQL lives in `local/hygiene.go` and `local/orphans.go`. The `hygiene/` package contains pure functions (validators, clusterers, orchestrators) that take typed scan results as input. This means:

- Clustering and validation logic is unit-testable without a DB (see `TestRunDuplicates_*`, `TestValidate*`, `TestInvalid_FromFieldValues`)
- SQL is covered separately by `local/*_test.go` against the synthetic fixture
- Real-library integration runs only under `SLOW=1` and is for eyeballing counts, not for regression detection

**Opt-in sub-checks.** Some checks are noisy or expensive and are excluded from the default set:

- `orphans --kind uncollected-item` — users who don't organize with collections get thousands of findings
- `orphans --kind missing-file --check-files` — stat's every imported attachment

Both are in `hygiene.AllOrphanKinds` but not in `defaultOrphanKinds`. The parser accepts them; the default run skips them.

**Duplicate detection.** DOI pass (exact match, score 1.0) subsumes title passes when members overlap. Title pass is two-stage: normalized-equality bucketing (always runs, ~free) and then length-windowed fuzzy over singletons (gated behind `DuplicatesOptions.Fuzzy`). The split matters: DOI + exact-title finish in ~300ms on 8.8k items, the fuzzy pass adds ~12s. `zot duplicates` defaults to fast; `--fuzzy` opts into the slow pass. `zot doctor --deep` does the same globally.

**Doctor command.** `zot doctor` (`internal/zot/doctor.go` + `cli/doctor.go`) is the read-only dashboard — runs invalid → missing → orphans → duplicates in that order (cheap/structural first, slow last) and prints a one-line summary per check plus an aggregate totals footer. It does NOT dump findings; users drill in via the per-check commands. `--check` narrows the run (repeatable, validated via `zot.ParseDoctorCheck`). `--deep` flips slow paths: fuzzy duplicates + `uncollected-item` orphan kind. It deliberately does NOT enable `--check-files` — stat'ing every attachment is another order of magnitude and stays a per-command opt-in.

**Adding a new hygiene check.** The pattern:

1. New SQL scan in `local/` returning a typed struct
2. Pure function in `hygiene/` that takes the scan output
3. DB-backed orchestrator `hygiene.X(db, opts) → *Report`
4. Result type in `hygieneresult.go` with `JSON()` + `Human()`
5. CLI command in `cli/hygiene.go` with `parseXFieldList` helper
6. Register in `cli/cli.go` `Commands()` factory
7. Tests: pure-function unit tests + fixture-backed SQL test + `SLOW=1`-gated real-library smoke

## Gotchas

- **Zotero date storage**: `itemDataValues.value` for the `date` field is stored as `"YYYY-MM-DD originalText"` — first token is the sortable form, second is the user's original input. `cleanDate()` in `readresult.go` strips everything after the first whitespace for display. Keep raw values in JSON output so downstream tools see Zotero's authentic data.
- **Zotero date `00` padding**: The sortable form pads unspecified components with `00`, not by truncating. A year-only entry is stored as `"1871-00-00 1871"`, not `"1871 1871"`. `ValidateDate` in `hygiene/invalid.go` treats `month=0` and `day=0` as "unspecified" markers — caught by the real-library smoke test after the first TDD pass flagged 4995 false positives.
- **Schema version drift**: `SchemaOutOfRange()` warns if `version.userdata` is outside `[MinTestedSchemaVersion, MaxTestedSchemaVersion]`. Current tested: 120–130 (live DB is 125 as of 2026-04-11). If queries start failing on a newer schema, widen the range only after verifying every query in `items.go` / `collections.go` / `tags.go`.
- **`tagFilter` vs `tag`**: `DeleteTagsParams.Tag` is a pipe-separated string (`"a || b || c"`), NOT a slice. API cap: 50 tags per request — see `DeleteTagsFromLibrary`'s batching.
- **BibTeX scope**: `exportBibTeX` is intentionally minimal. It reuses Better BibTeX's `citationKey` field when present (~all items in real libraries) and does only basic `{` escaping. For full LaTeX escaping, cite-key derivation, and edge-case handling, users should use Better BibTeX's own export from the desktop app.
- **`ZOT_REAL_DB` env var** / **`./zotero.sqlite`**: `local/realdb_test.go` uses `ZOT_REAL_DB`. Hygiene real-library tests open `./zotero.sqlite` at the repo root (gitignored, safe to mess with) and gate behind `SLOW=1`. Never hardcode the user's live library path.
- **Single-name creators**: Zotero stores institutional authors like "NASA" with `fieldMode=1` and the name in `lastName`. Our `Creator.Name` field carries these; `Creator.First`/`Last` stay empty. BibTeX emits them as `{NASA}` to suppress name parsing.
- **`DuplicateCandidate` lives in `local`, not `hygiene`**: type-aliased in `hygiene/duplicates.go` to avoid the circular import (`hygiene` imports `local`; `local` can't import `hygiene`). Same pattern applies if future checks need to share their scan types.
- **Fuzzy duplicate perf**: naive O(n²) Levenshtein over 5k singletons is multi-minute. `ClusterByTitle` sorts singletons by normalized-title length, breaks the inner loop once `lb > la/threshold`, and uses `levenshteinCapped` (DP aborts when row-min exceeds the edit budget). Together: ~12s on 8.8k items, workable interactively but still gated behind the `Fuzzy` flag. Fast mode (DOI + title-exact only, no fuzzy) is ~300ms on the same library and is the default for both `zot duplicates` and `zot doctor`.
- **Shared fixture**: `local/fixture_test.go` builds the synthetic `zotero.sqlite` once per `go test` invocation via `sync.Once` + `TestMain` cleanup. Safe because every test opens with `mode=ro&immutable=1`. Adding new tables/rows to the fixture may require updating `TestStats` and `TestListCollections` counts.

## Deferred — revisit next session

### Phase 6 (hygiene) — remaining
Five primitive checks landed: `missing`, `duplicates`, `invalid`, `orphans`, and the `doctor` umbrella. What's left:

- **`--fix` paths for `orphans`** — `--fix empty-collections` via existing `api.collections.Delete`, `--fix unused-tags` via `api.tags.DeleteTagsFromLibrary` batching. Both gated behind `cmdutil.ConfirmOrSkip` with a `--yes` bypass, matching the destructive-op pattern in `cli/write.go`.
- **`--fix trash` for duplicates** — only for DOI clusters (score 1.0, high confidence). Picks a keeper per cluster (item with more attachments / newer dateModified) and trashes the rest via `api.items.Trash`. Merging requires a Web API endpoint that the generated client doesn't currently expose.

### Phase 7
- **Tag rename** — requires fetching all items with the tag, per-item updates (new tag added + old removed), then `DeleteTagsFromLibrary([old])`. Doable, just tedious; skipped from Phase 5 to keep scope tight.
- **Groups library support** — everything is pinned to `libraries.type='user'`. Group libraries need a `libraryID` selector throughout `local/` and `api/`.
- **Rate-limit test realism** — the retry tests use synthetic `Retry-After` headers. The live API's actual rate limits have never been exercised. If we see 429s in practice, revisit `backoffDelay` and `maxRetry` defaults.
- **MCP server** — both reference Python projects ship one. sci-go has no MCP surface yet; if added, `cmd/zot/mcp.go` is the natural home.
- **PDF fulltext extraction + RAG** — zotero-cli-cc has this; `paper-study`/`study` user skills already overlap. Decide whether to duplicate or delegate before building.
- **Interactive TUI** — `sci zot tui`-style browser (dbtui pattern). Scoped out: v1 is CLI-only per the plan, so `sci zot` can be scripted.

### Things I'm not yet sure about
- **Export format fidelity**: should we emit real BibTeX via `better-bibtex.sqlite` (user has `~/Desktop/zotero/better-bibtex/`) instead of our own emitter? Worth a look in Phase 6 or 7 — would give us full fidelity for free, at the cost of another DB file dependency.
- **Write command confirmation UX**: destructive ops currently use `ConfirmOrSkip` with a single prompt. `sci cloud remove` double-confirms for some operations. If users accidentally trash items, revisit.
- **`zot add` interactive mode**: currently flag-only. A `huh`-based form for the no-flags case (like `zot setup`) would be nicer for one-off manual adds. Low priority.
- **File upload** (`linkMode=imported_file`) — the 4-step S3 dance from the OpenAPI header comment. Not in v1 because it bypasses `api.zotero.org` and needs multipart handling the generated client doesn't model.
