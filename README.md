# SciMinds Command Line Toolkit 

A small smart CLI toolkit for academic work on macOS in a single command: `sci`

Highlights:
- Wraps `brew` and `uv` tool installs to reliably auto-sync with a `Brewfile`
- TUIs for browsing: files, databases, hugging-face buckets, remote lab servers (ssh)
- Control Zotero from the command-line (write cloud, read local) with OpenAlex integration, pdf-to-markdown extraction, and LLM tools
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
|---------|--------------|
| [`sci py`](#sci-py) | Create ephemeral Python sessions and Marimo notebooks |
| [`sci proj`](#sci-proj) | Scaffold MyST/Quarto-flavored Python and writing projects |
| [`sci view`](#sci-view) | Interactive viewer for markdown, csv, sqlite, and duckdb files |
| [`sci db`](#sci-db) | Work with sqlite, duckdb, csv, json, and parquet files |
| [`sci vid`](#sci-vid) | Common video/audio editing operations (trim, resize, mute, …) |
| [`sci tools`](#sci-tools) | Manage `brew` and `uv` packages and keep Brewfile up-to-date |

### Help

| Command | What it does |
|---------|--------------|
| [`sci doctor`](#sci-doctor) | Check that your Mac is set up correctly |
| `sci update` | Update sci to the latest version |
| `sci help` | Interactive TUI with demos for any command |
| `sci learn` | Interactive TUI to learn common terminal commands |

### Cloud

| Command | What it does |
|---------|--------------|
| [`sci cloud`](#sci-cloud) | Up/download files to the SciMinds Hugging Face buckets (requires `hf auth`) |
| [`sci lab`](#sci-lab) | Up/download files to university HPC storage over SFTP (requires VPN) |

### Experimental

| Command | What it does |
|---------|--------------|
| [`sci cass`](#sci-cass) | Canvas LMS & GitHub Classroom management |
| [`sci zot`](#sci-zot) | Interact with Zotero and let an Agent drive |

---

## Examples

### `sci doctor`

Setup your Mac for scientific work — installs Homebrew if missing, walks you through `hf auth login` / `gh auth login`, and reports anything that needs attention.

<details>
![sci doctor](docs/casts/doctor.gif)
</details>

### `sci view`

Browse data files & markdown interactively.

<details>
<summary><b>usage</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
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
|---------|--------------|
| `sci proj new` | Create a new Python or writing project (`--kind python\|writing`) |
| `sci proj add` | Add packages to the project |
| `sci proj remove` | Remove packages from the project |
| `sci proj config` | Refresh config files in your project |
| `sci proj preview` | Start a live preview server for documents |
| `sci proj render` | Build documents into HTML or PDF |
| `sci proj run` | Run a project task |

`sci proj new` supports `--pkg-manager pixi|uv`, `--doc-system quarto|myst|none`, and `--template lab|default|<myst-template>` for picking a Typst flavor up front.

![sci proj](docs/casts/proj-new.gif)
</details>


### `sci py`

Ephemeral Python REPLs/notebooks and document-format conversion.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
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
|---------|--------------|
| `sci db create` | Create an empty database (SQLite or DuckDB, picked by extension) |
| `sci db reset` | Delete and recreate an empty database |
| `sci db info` | Show database metadata and tables |
| `sci db rename` | Rename a table or view |
| `sci db delete` | Delete a table or view |
| `sci db view <file>` | Interactively browse a database or tabular file (same as `sci view`) |

**Table / file inspection**

| Command | What it does |
|---------|--------------|
| `sci db head` | Show the first N rows of a tabular file |
| `sci db tail` | Show the last N rows of a tabular file |
| `sci db cols` | List column names and types |
| `sci db shape` | Report (rows, cols) |
| `sci db glimpse` | Transposed preview — one row per column with sample values |
| `sci db summarize` | Per-column statistics (min/max/avg/std/quartiles/null %) |
| `sci db query` | Run a read-only SELECT against a file (refer to it as `src`) |

**Import / convert**

| Command | What it does |
|---------|--------------|
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
|---------|--------------|
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
|---------|--------------|
| `sci lab setup` | Configure SSH access to lab storage |
| `sci lab ls` | List remote directory contents |
| `sci lab get <remote> [local]` | Download a file or directory (no arg → interactive browser) |
| `sci lab put <local> [remote]` | Upload a file or directory (no arg → interactive picker) |
| `sci lab connect` | Open an SSH shell in lab storage |

![sci lab](docs/casts/lab-browse.gif)
</details>


### `sci tools`

Manage Homebrew & uv tools via your Brewfile.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci tools list` | List packages in the Brewfile |
| `sci tools install` | Install packages from the Brewfile, or add and install a new package |
| `sci tools uninstall` | Remove a package from the Brewfile and uninstall it |
| `sci tools update` | Update the Homebrew registry and upgrade outdated packages |
| `sci tools outdated` | List outdated packages without upgrading |
| `sci tools reccs` | Pick optional tools to install |

![sci tools](docs/casts/tools-install.gif)
</details>


### `sci vid`

Common video/audio editing operations. Wraps `ffmpeg` with sensible defaults.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
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
|---------|--------------|
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

Zotero library management — local reads from `zotero.sqlite` (immutable, no contention with the running desktop app); writes go through the Zotero Web API. `sci zot guide` prints an agent-friendly cheat sheet of common workflows.

<details>
<summary><b>sub-commands</b> — click to expand</summary>

**Library scope.** Every zot command (except `setup`, `info`, `import`, `guide`, and `view`) requires `--library personal` or `--library shared`. Personal is your own Zotero user library; shared is a Zotero group library auto-detected at setup time. `sci zot info` without the flag summarizes both libraries side-by-side. Examples below include `--library personal` for the common case.

**Setup & overview**

| Command | What it does |
|---------|--------------|
| [`sci zot setup`](#setup--library-overview) | Save your Zotero API key + user ID + shared group (auto-detected) |
| [`sci zot info`](#setup--library-overview) | Summarize both libraries (personal + shared) |
| [`sci zot guide`](#setup--library-overview) | Agent-friendly cheat sheet of common workflows |
| [`sci zot --library personal view`](#browse) | Browse your library in an interactive table (read-only) |

**Search & export**

| Command | What it does |
|---------|--------------|
| [`sci zot --library personal search <query>`](#search--export) | Search the local library (`@field:` clauses for author/title/doi/pub/tag/type/year; `--remote` hits the Zotero Web API for fulltext + notes) |
| [`sci zot --library personal search <q> --export -o hits.bib`](#search--export) | Route search results through the export pipeline |
| [`sci zot --library personal export -o refs.bib`](#search--export) | Full-library BibTeX / CSL-JSON export (filters: `--collection`, `--tag`, `--type`) |
| [`sci zot --library personal find works <query>`](#search--export) | Look up papers on OpenAlex (no library round-trip) |
| [`sci zot --library personal find authors <query>`](#search--export) | Look up authors on OpenAlex |
| [`sci zot --library personal graph refs <key>`](#search--export) | Show works this item cites — in-library vs outside |
| [`sci zot --library personal graph cites <key>`](#search--export) | Show works that cite this item — in-library vs outside |

**Items**

| Command | What it does |
|---------|--------------|
| [`sci zot --library personal item read <key>`](#items) | Show full metadata for an item |
| [`sci zot --library personal item list`](#items) | List items with optional filters |
| [`sci zot --library personal item children <key>`](#items) | List child attachments + notes of an item |
| [`sci zot --library personal item export <key>`](#items) | Export a single item to CSL-JSON or BibTeX |
| [`sci zot --library personal item open <key>`](#items) | Open the item's PDF attachment |
| [`sci zot --library personal item attach <key> <path>`](#items) | Upload a local file as a new child attachment |
| [`sci zot --library shared item add` / `update` / `delete`](#items) | Create / patch / trash items via the Zotero Web API |
| [`sci zot --library personal item note add\|read\|list\|update`](#items) | Create and manage Zotero note items |
| [`sci zot import <path>`](#import-desktop-assisted-with-metadata-recognition) | Drag-drop equivalent via Zotero desktop: upload + auto-recognize metadata (CrossRef/arXiv) |

**PDF extraction (docling)**

| Command | What it does |
|---------|--------------|
| [`sci zot --library personal extract <key>`](#extraction--llm-workflows) | Convert the item's PDF into a Zotero child note (default is dry-run; `--apply` to post) |
| [`sci zot --library personal extract <key> --out DIR --apply`](#extraction--llm-workflows) | Full extraction: md + json + referenced PNGs + CSV tables to `DIR` |
| [`sci zot --library personal extract-lib`](#extraction--llm-workflows) | Bulk-extract every PDF in the library (default is cache-only; `--apply` to post notes; parallelizable with `-j`) |
| [`sci zot --library personal notes list\|read\|add\|update\|delete`](#extraction--llm-workflows) | Manage docling extraction notes |
| [`sci zot --library personal llm catalog\|query\|read`](#extraction--llm-workflows) | LLM-agent tools for querying docling notes |

**Organize**

| Command | What it does |
|---------|--------------|
| [`sci zot --library personal collection`](#organize) | Manage collections (list, create, delete, add/remove items) |
| [`sci zot --library personal tags`](#organize) | Manage tags (list, add/remove per item, delete library-wide) |
| [`sci zot --library personal saved-search`](#organize) | Manage Zotero saved searches (list, show, create, update, delete) |

**Hygiene**

| Command | What it does |
|---------|--------------|
| [`sci zot --library personal doctor`](#hygiene) | Run all hygiene checks (invalid → missing → orphans → duplicates → citekeys) |
| [`sci zot --library personal doctor {invalid,missing,orphans,duplicates,citekeys,dois}`](#hygiene) | Drill into individual hygiene reports |
| [`sci zot --library personal doctor pdfs`](#hygiene) | Find missing-PDF candidates via OpenAlex; `--collection` (local), `--saved-search NAME\|KEY` (live API), or `--keys-from FILE\|-` |
| [`sci zot --library personal doctor dois --fix --apply`](#hygiene) | Patch publisher-subobject DOIs (Frontiers `/abstract`, PLOS `.tNNN`, PNAS supplements) so OpenAlex resolves them |

`sci zot doctor --deep` enables fuzzy duplicate detection and noisier orphan kinds. `--library shared` routes the same surface to a Zotero group library (e.g. a shared lab collection) — `setup` picks the group automatically when the account belongs to exactly one, or accepts `--shared-group-id` when multiple groups exist.

**PDF → child note extraction.** `sci zot extract <KEY>` pipes the item's PDF attachment through [`docling`](https://github.com/DS4SD/docling) and posts the markdown as a child note on the parent — tagged `docling` and stamped with a sentinel comment so re-runs dedupe by sha256. Default is dry-run; pass `--apply` to actually create the note (`--html` posts rendered HTML instead of raw markdown). `--out DIR` switches to full extraction (md + json + referenced PNGs + CSV tables per `docling`'s always-on TableFormer) persisted for Obsidian-style vault exports; `--no-note` skips the Zotero post entirely. Identical re-runs skip; PDF updates PATCH-in-place so the note key stays stable. `sci zot notes delete` is the surgical undo — matches notes by their embedded sentinel (not tag). Requires `docling` on PATH (`sci doctor` installs it via `uv`).

For the bulk pipeline, `sci zot extract-lib` runs docling on every PDF in the library and **caches results locally without posting** by default — pass `--apply` to create the child notes. Cached output is reused on re-runs, so a failed run resumes where it left off; `--reextract` discards the cache.

**Library export details.** `sci zot export` honors user-pinned cite-keys (Zotero 7's native `citationKey` field, or legacy Better BibTeX `Citation Key:` lines in `extra`) and synthesizes semantic keys for everything else as `lastname{year}{firstword}-ZOTKEY`. The trailing 8-char Zotero key suffix guarantees uniqueness without collision arithmetic and keeps entries round-trippable back to the source item. Pinned entries also carry a `zotero://select/library/items/<KEY>` URI in the `note` field (appended to any existing user prose, never overwriting). A `.zotero-citekeymap.json` sidecar is written next to the output file; on the next run, any synthesized prefix that drifted (e.g. after a metadata typo fix) gets a biblatex `ids = {oldkey}` alias so manuscripts citing the old form still resolve.

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

#### Import (desktop-assisted, with metadata recognition)

![zot import](docs/casts/zot-import.gif)

#### Organize

![zot collection](docs/casts/zot-collection.gif)
![zot tags](docs/casts/zot-tags.gif)

#### Extraction & LLM workflows

![zot extract](docs/casts/zot-extract.gif)
![zot extract-lib](docs/casts/zot-extract-lib.gif)
![zot notes](docs/casts/zot-notes.gif)
![zot llm](docs/casts/zot-llm.gif)

#### Hygiene

![zot doctor](docs/casts/zot-doctor.gif)

</details>

---

## Releases

Every push to `main` and every PR runs the [Build & Release workflow](.github/workflows/release.yml):

1. **Check** — fmt, vet, lint, test
2. **Build** — cross-compiles `sci` for darwin/linux × arm64/amd64
3. **Publish** — uploads all binaries to a rolling `latest` GitHub release *(only when opted in via commit message, see Development below)*

Binaries are named `sci-{os}-{arch}` (e.g. `sci-darwin-arm64`, `sci-linux-amd64`).

**Updating:** Users run `sci update`, which compares the compiled-in commit SHA against the latest release and atomically replaces the binary if a newer build is available.

## Development

Prerequisites: [Go 1.26+](https://go.dev/dl/) and [just](https://github.com/casey/just) (`brew install just`).

You'll also need [`asciicinema`](https://docs.asciinema.org/manual/cli/quick-start/#__tabbed_1_3) to create new terminal "casts". Place sci command demos in `internal/help/casts/` and general terminal/git/python tutorials in `internal/learn/casts/`.

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

### CI commit-message triggers

Two opt-in actions are driven by strings in the commit message on `main`:

| Trigger | Effect |
|---|---|
| `[release]` | After the gate passes, publishes the build to the `latest` GitHub release. Without it, push/PR runs only fmt/vet/lint/test + cross-compile. |
| `[scenarios]` | Runs the [Environment Scenarios](.github/workflows/scenarios.yml) matrix (no-brew / brew-no-file / brew-file / no-brew-accept) for this commit. Otherwise scenarios only runs weekly (Mondays 09:00 UTC) or via manual dispatch. |

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

> A legacy Cloudflare R2 + worker auth flow was deprecated when this CLI moved to Hugging Face buckets. The old code (`internal/cloud/auth.go`, `device.go`, and `worker/`) is preserved on the `cloudflare-cloud` git branch; the deployed worker at `sci-auth.sciminds.workers.dev` can be decommissioned separately via `bunx wrangler delete` from a checkout of that branch.
