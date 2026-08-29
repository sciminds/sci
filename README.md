# SciMinds command line toolkit

A small smart CLI toolkit for academic work on macOS in a single command: `sci`

Highlights:

- Wraps `brew` and `uv` tool installs to reliably auto-sync with a `Brewfile`
- TUIs for browsing: files, databases, hugging-face buckets, remote lab servers (ssh)
- Query and cite your Zotero library read-only, with no account and no API key: search it, build a bibliography from exactly what a manuscript cites, export BibLaTeX, and run health checks that report and stop
- Control Canvas LMS from the command-line (experimental): manage assignments, modules, grades, files, etc

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh
```

Written in Go because:  

- Go is *typed* and *compiled* which makes it much faster than Python/JS *and* makes TDD with LLMs more reliable 
- Easy to create single-file programs that work on any computer
- No complicated dev tooling: everything is pretty standardized in the ecosystem and distribution is just GitHub
- Eshin wanted to learn a new language and it's particularly nice for agentic engineering

## Getting started

### Main

| Command | What it does |
| --------- | -------------- |
| [`sci py`](#sci-py) | Create ephemeral Python sessions and Marimo notebooks |
| [`sci proj`](#sci-proj) | Scaffold MyST/Quarto-flavored Python and writing projects |
| [`sci zot`](#sci-zot) | Query and cite your Zotero library, read-only |
| [`sci view`](#sci-view) | Interactive viewer for markdown, csv, json, parquet, sqlite, and duckdb files |
| [`sci db`](#sci-db) | Work with sqlite, duckdb, csv, json, and parquet files |
| [`sci vid`](#sci-vid) | Common video/audio editing operations (trim, resize, mute, …) |
| [`sci tools`](#sci-tools) | Manage `brew` and `uv` packages and keep Brewfile up-to-date |

### Help

| Command | What it does |
| --------- | -------------- |
| [`sci doctor`](#sci-doctor) | Check that your system is set up correctly |
| `sci setup` | Configure any sci tool (zot, lab, cass, …) from one menu |
| `sci update` | Update sci to the latest version |
| `sci help` | Interactive TUI with demos for any command |
| `sci learn` | Interactive TUI to learn common terminal commands |
| `sci version` | Print the installed version and build commit |

### Cloud

| Command | What it does |
| --------- | -------------- |
| [`sci cloud`](#sci-cloud) | Up/download files to the SciMinds Hugging Face buckets (requires `hf auth`) |
| [`sci lab`](#sci-lab) | Up/download files to university HPC storage over SFTP (requires VPN) |

### Experimental

| Command | What it does |
| --------- | -------------- |
| [`sci cass`](#sci-cass) | Canvas LMS & GitHub Classroom management |

---

## Examples

### `sci setup`

One front door over every tool's configuration. The menu lists each domain sci can configure with a ✓/✗ status mark, and drilling into one shows its current values before launching that tool's own wizard. Each tool still has its own entry point (`sci lab setup`, `sci zot setup`, …) — this is the hub over them. `sci setup --json` prints the same status non-interactively.

<details>
<summary><b>demo</b> — click to expand</summary>

![sci setup](docs/casts/setup.gif)
</details>

### `sci doctor`

Setup your Mac for scientific work — installs Homebrew if missing, walks you through `hf auth login` / `gh auth login`, and reports anything that needs attention.

`sci doctor --json` diagnoses and stops: it reports what is missing or outdated without installing or upgrading anything, so an agent can ask the question without changing the machine. Add `--yes` when you actually want the fresh-machine setup to run unattended.

<details>
<summary><b>demo</b> — click to expand</summary>

![sci doctor](docs/casts/doctor.gif)
</details>

### `sci update`

Self-update the `sci` binary in place. Downloads the latest release asset for your OS/arch, verifies its SHA256 against the release notes, and replaces the running binary atomically.

<details>
<summary><b>demo</b> — click to expand</summary>

![sci update](docs/casts/update.gif)
</details>

### `sci learn`

Interactive TUI for common terminal/git/Python commands — pick a topic, watch a rendered asciicast, repeat. Browses the casts under `internal/learn/casts/`.

Both `sci learn` and `sci help` need a terminal. Run either with stdin redirected, or with `--json`, and it exits 2 with a usage envelope naming a scriptable alternative rather than launching a TUI it cannot draw.

<details>
<summary><b>demo</b> — click to expand</summary>

![sci learn](docs/casts/learn.gif)
</details>

### `sci view`

Browse data files & markdown interactively.

<details>
<summary><b>usage</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci view <file>` | Browse a tabular file (CSV, JSON, SQLite, DuckDB, Parquet) or render a markdown document |
| `sci db view <file>` | Same viewer, mounted under the `sci db` namespace for discoverability |

Tabular files open in dbtui (`internal/tui/dbtui/`). Markdown files (`.md`, `.markdown`) render via the uikit markdown viewer — press `r` to reload from disk after external edits.

![sci view](docs/casts/view.gif)
</details>

### `sci proj`

Scaffold and manage Python data-analysis and writing projects.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci proj new` | Create a new Python or writing project (`--kind python\|writing`) |
| `sci proj add` | Add packages to the project |
| `sci proj remove` | Remove packages from the project |
| `sci proj config` | Refresh config files in your project |
| `sci proj preview` | Start a live preview server for documents |
| `sci proj render` | Build documents into HTML or PDF |
| `sci proj run` | Run a project task |

`sci proj new` supports `--pkg-manager pixi|uv`, `--doc-system quarto|myst|none`, `--md-layout single-file|composed`, and `--template lab|default|<myst-template>` for picking a Typst flavor up front.

![sci proj](docs/casts/proj-new.gif)
</details>

### `sci py`

Ephemeral Python REPLs/notebooks and document-format conversion.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci py repl` | Open a Python scratchpad |
| `sci py notebook` | Open a marimo notebook |
| `sci py convert` | Convert between marimo (.py), MyST (.md), and Quarto (.qmd) |

![sci py](docs/casts/py-repl.gif)
</details>

### `sci db`

Work with SQLite/DuckDB databases and tabular files (CSV, JSON, Parquet). Verbs dispatch on the file extension — pass a `.duckdb` and it routes through the bundled `duckdb` CLI; pass a `.csv`/`.parquet` and it goes through DuckDB's `read_csv_auto` / `read_parquet`.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

**Database management**

| Command | What it does |
| --------- | -------------- |
| `sci db create` | Create an empty database (SQLite or DuckDB, picked by extension) — refuses a path that already exists |
| `sci db reset` | Delete and recreate an empty database — refuses any path that is not a database |
| `sci db info` | Show database metadata and tables (read-only: the file's bytes are left untouched) |
| `sci db rename` | Rename a table or view |
| `sci db delete` | Delete a table or view |
| `sci db view <file>` | Interactively browse a database or tabular file (same as `sci view`) |

**Table / file inspection**

| Command | What it does |
| --------- | -------------- |
| `sci db head` | Show the first N rows of a tabular file |
| `sci db tail` | Show the last N rows of a tabular file |
| `sci db cols` | List column names and types |
| `sci db shape` | Report (rows, cols) |
| `sci db glimpse` | Transposed preview — one row per column with sample values |
| `sci db summarize` | Per-column statistics (min/max/avg/std/quartiles/null %) |
| `sci db query` | Run a read-only SELECT (databases: real table names; flat files: refer to the file as `src`) |

**Import / convert**

| Command | What it does |
| --------- | -------------- |
| `sci db add` | Import a CSV as a new table (errors if the table already exists) |
| `sci db append` | Append CSV rows to an existing table |
| `sci db convert` | Convert between csv/tsv/json/jsonl/parquet/sqlite/duckdb |

![sci db](docs/casts/db-add.gif)
</details>

### `sci cloud`

Upload/download files to the SciMinds Hugging Face buckets. Every verb defaults to the **private** bucket (`sciminds/private`); pass `--public` to operate against the world-readable bucket (`sciminds/public`). Files are keyed as `<username>/<filename>` so per-user listings stay scoped.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci cloud setup` | Authenticate with Hugging Face (requires sciminds org membership) |
| `sci cloud ls` | List shared files (default: private; `--public` to list public) |
| `sci cloud get <name> [local]` | Download a shared file (no arg → interactive browser) |
| `sci cloud put <file>` | Upload a file (default: private; `--public` shares + returns an HTTPS URL) |
| `sci cloud remove <name>` | Remove a shared file |

![sci cloud](docs/casts/cloud-put.gif)
</details>

### `sci lab`

Access university lab storage over SFTP (VPN required).

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci lab setup` | Configure SSH access to lab storage |
| `sci lab ls` | List remote directory contents |
| `sci lab get <remote> [local]` | Download a file or directory (no arg → interactive browser) |
| `sci lab put <local> [remote]` | Upload a file or directory (no arg → interactive picker) |
| `sci lab connect` | Open an SSH shell in lab storage |

![sci lab](docs/casts/lab-get.gif)
</details>

### `sci tools`

Manage Homebrew & uv tools via your Brewfile.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci tools list` | List packages in the Brewfile |
| `sci tools install` | Install packages from the Brewfile, or add and install a new package |
| `sci tools uninstall` | Remove a package from the Brewfile and uninstall it |
| `sci tools update` | Update the Homebrew registry and upgrade outdated packages |
| `sci tools outdated` | List outdated packages without upgrading |
| `sci tools reccs` | Pick recommended tools to install |
| `sci tools apps` | Pick recommended GUI apps (casks) to install |

![sci tools](docs/casts/tools-install.gif)
</details>

### `sci vid`

Common video/audio editing operations. Wraps `ffmpeg` with sensible defaults.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci vid info` | Show video info (resolution, duration, codec, fps, size) |
| `sci vid cut` | Trim a segment (e.g. `0:30 1:00`) |
| `sci vid compress` | Shrink a video file (reduce file size) |
| `sci vid convert` | Convert to another format (mp4, webm, etc.) |
| `sci vid gif` | Convert to optimized GIF |
| `sci vid resize` | Scale video (720p, 1080p, 4k, 50%, W:H) |
| `sci vid speed` | Change playback speed (e.g. `2` = 2x faster) |
| `sci vid mute` | Remove audio from a video |
| `sci vid extract-audio` | Extract audio track to file |
| `sci vid strip-subs` | Remove subtitles from a video |

![sci vid](docs/casts/vid-cut.gif)
</details>

### `sci cass`

Canvas LMS & GitHub Classroom management.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
| --------- | -------------- |
| `sci cass setup` | Save your Canvas API token (one-time) |
| `sci cass init` | Create a `cass.yaml` config for a course directory |
| `sci cass pull` | Fetch students, assignments, and submissions from Canvas/GitHub |
| `sci cass status` | Show sync status, pending changes, and discrepancies |
| `sci cass diff` | Show pending grade changes (local or `--remote` 3-way) |
| `sci cass push` | Push grade changes to Canvas |
| `sci cass match` | Interactively match GitHub usernames to Canvas students |
| `sci cass revert` | Discard unpushed grade edits |
| `sci cass log` | Show operation history |
| `sci cass canvas modules` | List, create, publish, or delete course modules |
| `sci cass canvas assignments` | List, create, publish, or delete assignments |
| `sci cass canvas announce` | List, post, or delete announcements |
| `sci cass canvas files` | List course files |

Syncs course data to a local SQLite database (`cass.db`) with a git-like workflow: pull shows changelogs, diff shows pending grade changes, push sends grades to Canvas with conflict detection. GitHub Classroom is optional — works with Canvas-only courses.

![sci cass](docs/casts/cass-pull.gif)
</details>

### `sci zot`

**Query and cite your Zotero library, read-only — no account, no API key, no network.** Every command opens the `zotero.sqlite` already sitting on your disk, in immutable mode: nothing to log into, no contention with the running desktop app, and nothing sci can do to your library. Search it, build a bibliography from what a manuscript actually cites, export the whole thing to BibLaTeX, and run health checks that report and stop. `sci zot guide` prints an agent-friendly cheat sheet of the same surface, and `sci zot guide --json` is its machine-readable contract.

Nothing here writes. No verb creates, edits, or deletes an item, a collection, a tag, or a note — editing your library stays Zotero's job, and sci only ever reads what Zotero has already written down.

`sci zot setup` records exactly one thing: the directory holding `zotero.sqlite`. The trade for that simplicity is staleness — reading the file in immutable mode means anything Zotero has committed but not yet checkpointed, or not yet synced down from the server, is invisible until the desktop app catches up. sci reports that gap rather than hiding it, as a `stale-local` warning on the results it affects.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

**Library scope.** Every zot command (except `setup` and `guide`) runs against `--library personal` or `--library shared`. Personal is your own Zotero user library; shared is a Zotero group library, read straight out of the same local database at setup time. When the flag is omitted, sci auto-selects the only configured library, or prompts when both are configured. `search`, `bib`, `browse`, and `export --format ndjson` also accept `--library all` — a merged read pool across both libraries with per-row provenance. `sci zot info` without the flag summarizes both libraries side-by-side. Examples below include `--library personal` for the common case.

**Setup & overview**

| Command | What it does |
| --------- | -------------- |
| [`sci zot setup`](#setup--library-overview) | Point sci at your Zotero data directory — the only thing sci stores |
| [`sci zot info`](#setup--library-overview) | Summarize both libraries (personal + shared) |
| [`sci zot guide`](#setup--library-overview) | Agent-friendly cheat sheet of common workflows |
| [`sci zot --library personal view`](#browse) | Browse your library in an interactive table (read-only) |

**Search & export**

| Command | What it does |
| --------- | -------------- |
| [`sci zot --library personal search <query>`](#search--export) | Search the local library — free-text words AND across metadata fields; `@field:` clauses for author/title/doi/pub/tag/type/year/citekey; a bare year like `2021` filters by year; `--full` hydrates each hit with abstract, cite-key, and authors |
| [`sci zot browse`](#search--export) | Interactive search REPL — type to search, type a hit number to open its PDF |
| [`sci zot --library personal search <q> --export -o hits.bib`](#search--export) | Route search results through the export pipeline |
| [`sci zot --library personal bib <file-or-dir>`](#search--export) | Build a bibliography from the `@citekeys`, DOIs, and links cited in markdown/Quarto files (`--recursive`); refs matching 0 or >1 items are always listed, never guessed |
| [`sci zot --library personal export -o refs.bib`](#search--export) | Full-library BibLaTeX / CSL-JSON export, or `--format ndjson` for a kind-tagged item-plane mirror (filters: `--collection`, `--tag`, `--type`) |

**Items**

| Command | What it does |
| --------- | -------------- |
| [`sci zot --library personal item read <key>...`](#items) | Show full metadata for one or more items (`--missing-ok` reports not-founds instead of failing the batch) |
| [`sci zot --library personal item list`](#items) | List items with optional filters |
| [`sci zot --library personal item children <key>`](#items) | List child attachments + notes of an item |
| [`sci zot --library personal item export <key>`](#items) | Export a single item to CSL-JSON or BibLaTeX |
| [`sci zot --library personal item open <key>`](#items) | Open the item's PDF attachment |
| [`sci zot --library personal notes list\|read`](#items) | The notes *you* wrote — machine-generated full-text extractions are filtered out (`--md` renders a note's body as markdown) |
| [`sci zot --library personal link list <key>`](#items) | Show an item's "related items" relations |

**Organize**

| Command | What it does |
| --------- | -------------- |
| [`sci zot --library personal collection list\|browse`](#organize) | List every collection with item counts, or browse collections and their items |
| [`sci zot --library personal tags list\|browse`](#organize) | List every tag with usage counts, or browse tags and their items |

**Hygiene**

| Command | What it does |
| --------- | -------------- |
| [`sci zot --library personal doctor`](#hygiene) | Run all hygiene checks (invalid → missing → orphans → duplicates → citekeys) |
| [`sci zot --library personal doctor {invalid,missing,orphans,duplicates,citekeys,dois}`](#hygiene) | Drill into individual hygiene reports |
| [`sci zot --library personal doctor dois`](#hygiene) | Flag publisher-subobject DOIs (Frontiers `/abstract`, PLOS `.tNNN`, PNAS supplements) — the ones metadata APIs 404 on |

**Doctor reports; it does not repair.** Every check reads the local Zotero database and stops there: no writes, no network, no metered lookups. That is the whole contract — a health check you can run on any machine, against a library you have not synced, without a credential. Findings name the item and the field they are about, so the fix is one you make deliberately in Zotero rather than one a CLI applies to 5,000 rows while you watch.

`sci zot doctor --deep` enables fuzzy duplicate detection and noisier orphan kinds. `--library shared` routes the same surface to a Zotero group library (e.g. a shared lab collection) — `setup` reads the group out of the local database automatically when there is exactly one, or takes `--shared-group-id` when the library holds several.

**Library export details.** `sci zot export` honors user-pinned cite-keys (Zotero 7's native `citationKey` field, or legacy Better BibTeX `Citation Key:` lines in `extra`) and synthesizes semantic keys for everything else as `{lastname}{year}-{words}-{ZOTKEY}` (e.g. `miao2026-commdistneur-MB5NEVSP`). The trailing 8-char Zotero key suffix guarantees uniqueness without collision arithmetic and keeps entries round-trippable back to the source item. Pinned entries also carry a `zotero://select/library/items/<KEY>` URI in the `note` field (appended to any existing user prose, never overwriting). A `.zotero-citekeymap.json` sidecar is written next to the output file; on the next run, any synthesized prefix that drifted (e.g. after a metadata typo fix) gets a biblatex `ids = {oldkey}` alias so manuscripts citing the old form still resolve.

#### Setup & library overview

![zot setup + info](docs/casts/zot-setup.gif)
![zot info](docs/casts/zot-info.gif)

#### Browse

![zot view](docs/casts/zot-view.gif)

#### Search & export

![zot search](docs/casts/zot-search.gif)
![zot export](docs/casts/zot-export.gif)

#### Items

![zot item](docs/casts/zot-item.gif)
![zot notes](docs/casts/zot-notes.gif)

#### Organize

![zot collection](docs/casts/zot-collection.gif)
![zot tags](docs/casts/zot-tags.gif)

#### Hygiene

![zot doctor](docs/casts/zot-doctor.gif)

</details>

---

## Releases

Every push to `main` and every PR runs the [Build & Release workflow](.github/workflows/release.yml):

1. **Check** — fmt, vet, lint, test
2. **Build** — cross-compiles `sci` for darwin/linux × arm64/amd64
3. **Publish** — uploads all binaries to an immutable CalVer-tagged GitHub release (`vYYYY.MM.DD`, `.N` suffix for same-day follow-ups) *(only when opted in via commit message, see Development below)*. `releases/latest` always points at the newest one, and old versions stay downloadable for rollback.

Binaries are named `sci-{os}-{arch}` (e.g. `sci-darwin-arm64`, `sci-linux-amd64`).

**Updating:** Users run `sci update`, which compares the compiled-in CalVer version against the latest release tag and atomically replaces the binary only when the release is strictly newer — a binary ahead of the published release is never downgraded.

## Development

Prerequisites: [Go 1.26+](https://go.dev/dl/) and [just](https://github.com/casey/just) (`brew install just`).

You'll also need [`asciinema`](https://docs.asciinema.org/manual/cli/quick-start/#__tabbed_1_3) to create new terminal "casts". Place sci command demos in `internal/help/casts/` and general terminal/git/python tutorials in `internal/learn/casts/`.

*This is also set up as a git pre-commit hook:*

```bash
# Run the full check suite (fmt, vet, lint, test, build)
just ok
```

Launch auto-documentation site:

```bash
just docs
```

To try commands during development:

```bash
just run doctor       # same as: go run ./cmd/sci doctor
just run proj new     # etc.
```

For a longer co-develop loop, `just install` symlinks the repo build to `~/.local/bin/sci`, so every `just build` / `just ok` immediately *is* the installed `sci`. `sci update` harmlessly takes the machine back out of dev mode (it replaces the symlink, never the repo build); re-run `just install` to return.

### CI commit-message triggers

Two opt-in actions are driven by strings in the commit message on `main`:

| Trigger | Effect |
| --- | --- |
| `[release]` | After the gate passes, publishes a CalVer-tagged GitHub release (`vYYYY.MM.DD[.N]`). Without it, push/PR runs only fmt/vet/lint/test + cross-compile. |
| `[scenarios]` | Runs the [Environment Scenarios](.github/workflows/scenarios.yml) matrix (no-brew / brew-no-file / brew-file / no-brew-accept) for this commit. The matrix also auto-runs when a push touches the surfaces it asserts against (cmdutil/brew/doctor/netutil, the tools/doctor commands, or the workflow file); otherwise it runs weekly (Mondays 09:00 UTC) or via manual dispatch. |

Markers are matched as substrings (same convention as `[skip ci]`); the brackets keep them visually distinct from prose so describing them in the commit body doesn't fire them accidentally.

Combine both in one commit if a release touches brew/doctor/tools code and you want scenario coverage *before* it ships.

### Cloud auth infrastructure

`sci cloud` shells out to the `hf` CLI for all bucket operations against the SciMinds Hugging Face org. Auth is delegated entirely to `hf auth login` — sci stores no tokens.

**Components:**

1. **`hf` CLI** — installed via `uv tool install hf` (wired through doctor's Brewfile). Auth state lives in `~/.cache/huggingface/`.
2. **`git-xet`** — required for HF's Xet-protocol transfers. Installed via `brew install git-xet` and registered globally with `git xet install`. Both are gated by `sci doctor`.
3. **Org buckets:**
   - `sciminds/public` — world-readable; uploads return an HTTPS URL of the form `https://huggingface.co/buckets/sciminds/<bucket>/resolve/<username>/<filename>`.
   - `sciminds/private` — org-members-only; default for `sci cloud put`.

Files are keyed as `<username>/<filename>` within each bucket so per-user listings stay scoped.

**Onboarding a new sciminds member:**

```bash
hf auth login                              # paste an HF token with read+write on sciminds
git xet install                            # one-time global LFS transfer agent setup
sci doctor                                 # verify everything's green
```
