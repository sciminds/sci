# CLAUDE.md — zot (internal/zot/)

Zotero library management. Also installable standalone via `cmd/zot/` (mirrors the `dbtui` / `markdb` pattern).

For the package layout, command tree, and type definitions, read the source — `cli/cli.go`, `result.go`, `hygiene/hygiene.go` are the entry points.

## Two surfaces, one command tree

The full urfave/cli v3 tree lives in `internal/zot/cli.Commands()`. Both entry points import it:

- `cmd/zot/main.go` — standalone `zot` binary
- `cmd/sci/zot.go` — `sci zot …` subcommand

Any subcommand added to `internal/zot/cli` shows up in both surfaces automatically. **Never duplicate wiring.**

## Reads local, writes cloud (load-bearing split)

- **Reads** → `internal/zot/local` opens `zotero.sqlite` with `file:…?mode=ro&immutable=1&_pragma=query_only(1)`. Immutable mode skips WAL processing entirely so we never contend with the running Zotero desktop app's locks.
- **Writes** → `internal/zot/api` calls the Zotero Web API. We do **NOT** wait for local sync-back — Zotero desktop listens on its own sync stream and updates `zotero.sqlite` automatically. API success = done from our side.

**Corollary:** immediately after a write the local DB will briefly diverge from the server. That's fine. Don't add polling or consistency checks.

## Generated client (`client/`)

Regenerate with `just zot-gen`. The recipe reads `~/Documents/webapps/apis/zotero/openapi.yaml` (OpenAPI 3.1), rewrites it to 3.0 on the fly because oapi-codegen v2 doesn't support 3.1 unions, then runs `oapi-codegen` and gofmts. If the upstream spec grows new 3.1-isms or name collisions, extend the `sd` pipeline in the justfile rather than hand-editing.

**Never hand-edit `zotero.gen.go`.** Any needed surface goes through `internal/zot/api`.

## 412 Precondition Failed pattern

Zotero uses optimistic concurrency: writes carry `If-Unmodified-Since-Version` (header) or `version` (body), and the API returns 412 if the target advanced.

**Why it's not in middleware:** recovering from a 412 requires re-reading the object to get the fresh version AND rebuilding the request payload — `internal/zot/api/retry.go` only knows HTTP, not object semantics. So 412 recovery is per-operation.

**The helper:** `withVersionRetry(fn, getVersion, initial)` in `items.go`. `fn(ver)` performs the write; on `*VersionConflictError`, `getVersion()` refreshes and `fn` is called once more. More than one retry would indicate a hot-contention loop and we'd rather surface it. Every write op owns its own `getVersion` closure so the refresh path is explicit at the call site.

## Hygiene checks

Read-only library-quality checks. All four checks (`invalid`, `missing`, `orphans`, `duplicates`) live as sub-commands of `zot doctor`; bare `zot doctor` runs every check and prints an aggregate dashboard, while `zot doctor <check>` drills into a single one with per-finding detail. The parent command has both `Action` (the aggregate run) and `Commands` (the four leaves) — urfave/cli v3 dispatches to a leaf when its name is the first positional arg, otherwise runs Action; `--help` is intercepted before either, so `zot doctor --help` prints the sub-command menu. SQL lives in `local/hygiene.go` and `local/orphans.go`; pure logic (validators, clusterers) in `hygiene/`. The split makes clustering and validation unit-testable without a DB.

**Report shape.** Every check returns `*hygiene.Report{Check, Scanned, Findings, Clusters, Stats}`. `Stats` is a per-check struct (`MissingStats`, `DuplicatesStats`, `InvalidStats`, `OrphansStats`) read by renderers via type assertion.

**Severity taxonomy** (consistent across checks):

- `SevError` — structurally broken (missing title, attachment file gone from disk)
- `SevWarn` — citation-affecting (missing creators/date, malformed DOI/URL/date, standalone attachment)
- `SevInfo` — coverage gaps and user-workflow choices

**Doctor ordering:** invalid → missing → orphans → duplicates (cheap/structural first, slow last). Prints one-line summary per check + aggregate footer. Does NOT dump findings — users drill in via per-check commands.

**`--deep` flips slow paths** (fuzzy duplicates + `uncollected-item` orphan kind). Deliberately does NOT enable `--check-files` — stat'ing every attachment is another order of magnitude and stays a per-command opt-in.

**Opt-in sub-checks** (in `hygiene.AllOrphanKinds` but not `defaultOrphanKinds`):

- `orphans --kind uncollected-item` — users without collections get thousands of findings
- `orphans --kind missing-file --check-files` — stat's every imported attachment

**Duplicate detection:** DOI pass (exact, score 1.0) subsumes title passes when members overlap. Title pass is two-stage: normalized-equality bucketing (always runs, ~free) and length-windowed fuzzy over singletons (gated behind `DuplicatesOptions.Fuzzy`). Fast mode ~300ms on 8.8k items; fuzzy adds ~12s. Both `zot doctor duplicates` and `zot doctor` default to fast.

## Conventions

- **Raw `database/sql` in `local/`** — same exception family as `dbtui`/`markdb`/`board`. Local reads are perf-sensitive and don't need dbx ergonomics.
- **All inputs validated at the command layer.** `internal/zot.Setup()` expects pre-validated args; interactive prompting and `--json` non-interactive validation both live in `cli/setup.go`.
- **Every write command short-circuits via `requireAPIClient()`** — checks `RequireConfig()` + `netutil.Online()` before building the API client. Destructive ops go through `cmdutil.ConfirmOrSkip` with `--yes` bypass.
- **`--json` mode is non-interactive.** `setup` requires `--api` + `--library` when `--json` is set. Any new prompting command must do the same check.

## Gotchas

- **Zotero date storage**: `itemDataValues.value` for the `date` field is `"YYYY-MM-DD originalText"` — first token sortable, second is user input. `cleanDate()` strips after the first whitespace for display. Keep raw values in JSON output so downstream tools see authentic data.
- **Zotero date `00` padding**: the sortable form pads unspecified components with `00`, not by truncating. Year-only is `"1871-00-00 1871"`, not `"1871 1871"`. `ValidateDate` treats `month=0`/`day=0` as "unspecified" markers — caught by the real-library smoke test after the first TDD pass flagged 4995 false positives.
- **Schema version drift**: `SchemaOutOfRange()` warns if `version.userdata` is outside `[MinTestedSchemaVersion, MaxTestedSchemaVersion]`. Current tested 120–130 (live DB is 125 as of 2026-04-11). Widen only after verifying every query in `items.go` / `collections.go` / `tags.go`.
- **`tagFilter` vs `tag`**: `DeleteTagsParams.Tag` is a pipe-separated string (`"a || b || c"`), NOT a slice. API caps 50 tags per request — see `DeleteTagsFromLibrary`'s batching.
- **BibTeX export** (`export.go` + `exportlib.go` + `citekey/`): cite-key policy lives in the `citekey` sub-package (split out to break the `zot → zot/hygiene` import cycle so `hygiene.Citekeys` can also call `citekey.Validate`). The v2 synthesized format is `{author}{year}-{words}-{ZOTKEY}` where `{words}` is up to three non-stopword title tokens each truncated to 4 chars (see citekey/citekey.go for the stopword list / ASCII-fold / wordCount/wordMaxLen constants — changing either rewrites every synthesized key so treat as breaking). `citekey.Resolve` walks: native `citationKey` → legacy BBT `Citation Key:` in `extra` → `citekey.Synthesize`. Single-item writer `writeBibEntry` is shared by `zot item export` and the library exporter `ExportLibrary`. Pinned entries append `zotero://select/library/items/<KEY>` to the `note` field; user-authored `extra` prose is preserved via `citekey.ExtractNote` (strips `Citation Key:` lines). Drift detection: `ExportLibrary` takes a prior `Keymap` and emits biblatex `ids = {oldkey}` when a synthesized prefix changed between runs. `cli/export.go` persists `.zotero-citekeymap.json` next to `-o FILE` and skips the sidecar write when `len(stats.Keymap) == 0` to avoid clobbering an existing file from a different export. Only `{`/`\` are escaped — for full LaTeX escaping users should use Better BibTeX's own export.
- **Cite-key hygiene** (`hygiene/citekeys.go`): read-only check that grades every stored cite-key against `citekey.Validate`. Findings bucket into `invalid` (SevError — empty, whitespace, or BibTeX-illegal chars like `{}%#~,=\"\\`), `non-canonical` (SevWarn — legal but doesn't match the v2 regex, expected for BBT-managed libraries), and `collision` (SevError — two items share a cite-key). Items with no stored key contribute to `Unstored` in stats but emit no finding — the (not-yet-built) fix command is where unstored items get materialized. Wired into `zot doctor` as the last check (touches every row). The Web-API writer path for fix lands in a later slice.
- **`ZOT_REAL_DB` env var** / **`./zotero.sqlite`**: `local/realdb_test.go` uses `ZOT_REAL_DB`. Hygiene real-library tests open `./zotero.sqlite` at the repo root (gitignored, safe to mess with) and gate behind `SLOW=1`. Never hardcode the user's live library path.
- **Single-name creators**: institutional authors like "NASA" are stored with `fieldMode=1` and the name in `lastName`. Our `Creator.Name` field carries these; `Creator.First`/`Last` stay empty. BibTeX emits them as `{NASA}` to suppress name parsing.
- **`DuplicateCandidate` lives in `local`, not `hygiene`** — type-aliased in `hygiene/duplicates.go` to avoid the circular import (`hygiene` imports `local`; `local` can't import `hygiene`). Same pattern applies if future checks share scan types.
- **Fuzzy duplicate perf**: naive O(n²) Levenshtein over 5k singletons is multi-minute. `ClusterByTitle` sorts by normalized-title length, breaks the inner loop once `lb > la/threshold`, and uses `levenshteinCapped` (DP aborts when row-min exceeds the edit budget). Together: ~12s on 8.8k items, still gated behind `Fuzzy`.
- **Shared fixture**: `local/fixture_test.go` builds the synthetic `zotero.sqlite` once per `go test` invocation via `sync.Once` + `TestMain` cleanup. Safe because every test opens with `mode=ro&immutable=1`. Adding new tables/rows may require updating `TestStats` and `TestListCollections` counts.
