# SciMinds Command Line Toolkit 

A small smart CLI toolkit for academic work on macOS in a single command: `sci`

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh
```

Written in Go because:  
- Go is *typed* and *compiled* which makes it much faster than Python/JS *and* makes TDD with LLMs more reliable 
- Easy to create single-file programs that work on any computer
- No complicated dev tooling: everything is pretty standardized in the ecosystem and distribution is just GitHub
- Eshin wanted to learn a new language

## What can it do?

### Getting started

| Command | What it does |
|---------|--------------|
| `sci doctor` | Check that your Mac is set up correctly |
| `sci help` | Interactive TUI with demos for any command |
| `sci learn` | Interactive TUI to learn common terminal cmds |
| `sci update` | Update sci to the latest version |

![sci doctor](docs/casts/doctor.gif)

### `sci view` - Browse data files & markdown

<details>
<summary><b>usage</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci view <file>` | Interactively browse a tabular data file (CSV, JSON, SQLite) or render a markdown document |

Tabular files open in dbtui (`internal/tui/dbtui/`), also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/dbtui@latest`. Markdown files (`.md`, `.markdown`) render via the uikit markdown viewer — press `r` to reload from disk after external edits.

</details>

![sci view](docs/casts/view.gif)

### `sci proj` - Manage Python projects

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci proj new` | Create a new Python project |
| `sci proj add` | Add packages to the project |
| `sci proj remove` | Remove packages from the project |
| `sci proj config` | Refresh config files in your project |
| `sci proj preview` | Start a live preview server for documents |
| `sci proj render` | Build documents into HTML or PDF |
| `sci proj run` | Run a project task |


</details>

![sci proj](docs/casts/proj-new.gif)

### `sci py` - ephemeral python repls/notebooks & file conversion

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci py repl` | Open a Python scratchpad |
| `sci py notebook` | Open a marimo notebook |
| `sci py convert` | Convert between marimo (.py), MyST (.md), and Quarto (.qmd) |

</details>

![sci py](docs/casts/py-repl.gif)

### `sci db` - Manage databases

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci db create` | Create an empty database |
| `sci db info` | Show database metadata and tables |
| `sci db add` | Import CSV files into a database |
| `sci db rename` | Rename a table in a database |
| `sci db delete` | Delete a table from a database |
| `sci db reset` | Delete and recreate an empty database |

</details>

![sci db](docs/casts/db-add.gif)

### `sci cloud` - public cloud storage

<details>
<summary><b>sub-commands</b> — click to expand</summary>

| Command | What it does |
|---------|--------------|
| `sci cloud setup` | Authenticate with Hugging Face (requires sciminds org membership) |
| `sci cloud ls` | List shared files |
| `sci cloud get <name> [local]` | Download a shared file |
| `sci cloud put <file>` | Upload a file to cloud storage |
| `sci cloud browse` | Interactively browse shared files |
| `sci cloud remove <name>` | Remove a shared file |

</details>

![sci cloud](docs/casts/cloud-put.gif)

### `sci lab` - lab storage (sftp)

<details>
<summary><b>sub-commands</b> — click to expand</summary>


| Command | What it does |
|---------|--------------|
| `sci lab setup` | Configure SSH access to lab storage |
| `sci lab ls` | List remote directory contents |
| `sci lab get` | Download a file or directory from lab storage |
| `sci lab put` | Upload a file or directory to your lab space |
| `sci lab browse` | Interactively browse lab storage and download folders |
| `sci lab connect` | Open an SSH shell in lab storage |


</details>

![sci lab](docs/casts/lab-browse.gif)

### `sci tools` - manage tools (Homebrew & uv)

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

</details>

![sci tools](docs/casts/tools-install.gif)

### `sci vid` - Video/audio editing

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

</details>

![sci vid](docs/casts/vid-cut.gif)

### `sci cass` - Canvas LMS & GitHub Classroom

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

</details>

![sci cass](docs/casts/cass-pull.gif)

### `sci zot` - Zotero library management (Experimental)

<details>
<summary><b>sub-commands</b> — click to expand</summary>


**Library scope.** Every zot command (except `setup` and `info`) requires `--library personal` or `--library shared`. Personal is your own Zotero user library; shared is a Zotero group library auto-detected at setup time. `sci zot info` without the flag summarizes both libraries side-by-side. Examples below include `--library personal` for the common case.

| Command | What it does |
|---------|--------------|
| `sci zot setup` | Save your Zotero API key + user ID + shared group (auto-detected) |
| `sci zot info` | Summarize both libraries (personal + shared) |
| `sci zot --library personal info` | Narrow summary to the personal library |
| `sci zot --library personal search <query>` | Search the local Zotero library |
| `sci zot --library personal search <q> --export -o hits.bib` | Route search results through the export pipeline |
| `sci zot --library personal export -o refs.bib` | Full-library BibTeX / CSL-JSON export (filters: `--collection`, `--tag`, `--type`) |
| `sci zot --library personal item read <key>` | Show full metadata for an item |
| `sci zot --library personal item list` | List items with optional filters |
| `sci zot --library personal item children <key>` | List child attachments + notes of an item |
| `sci zot --library personal item export <key>` | Export a single item to CSL-JSON or BibTeX |
| `sci zot --library personal item open <key>` | Open the item's PDF attachment |
| `sci zot --library personal item extract <key>` | Convert the item's PDF into a Zotero child note (via `docling`) |
| `sci zot --library personal item extract <key> --out DIR` | Full extraction: md + json + referenced PNGs + CSV tables to DIR |
| `sci zot --library personal item extract <key> --delete` | Undo: trash any note carrying this PDF's sci-extract sentinel |
| `sci zot --library shared item add` / `update` / `delete` | Create / patch / trash items via the Zotero Web API |
| `sci zot --library personal item attach <key> <path>` | Upload a local file as a new child attachment of an existing item |
| `sci zot import <path>` | Drag-drop equivalent via Zotero desktop: upload + auto-recognize metadata (CrossRef/arXiv) |
| `sci zot --library shared collection` / `tags` | Manage collections and tags in the shared group library |
| `sci zot --library personal doctor` | Run all hygiene checks (invalid → missing → orphans → duplicates → citekeys) |
| `sci zot --library personal doctor {invalid,missing,orphans,duplicates,citekeys,dois}` | Drill into individual hygiene reports |
| `sci zot --library personal doctor pdfs` | Find missing-PDF candidates via OpenAlex; `--collection` (local), `--saved-search NAME\|KEY` (live API), or `--keys-from FILE\|-` |
| `sci zot --library personal doctor dois --fix --apply` | Patch publisher-subobject DOIs (Frontiers `/abstract`, PLOS `.tNNN`, PNAS supplements) so OpenAlex resolves them |

Reads the local `zotero.sqlite` (immutable, no contention with the running Zotero desktop app); writes go through the Zotero Web API. `sci zot doctor --deep` enables fuzzy duplicate detection and noisier orphan kinds. `--library shared` routes the same surface to a Zotero group library (e.g. a shared lab collection) — `setup` picks the group automatically when the account belongs to exactly one, or accepts `--shared-group-id` when multiple groups exist.

**PDF → child note extraction.** `sci zot item extract <KEY>` pipes the item's PDF attachment through [`docling`](https://github.com/DS4SD/docling), renders the markdown as HTML, and posts it as a child note on the parent — tagged `docling` and stamped with a sentinel comment so re-runs dedupe by sha256. Default mode produces a clean, Zotero-friendly note from a temp dir. `--out DIR` switches to full extraction (md + json + referenced PNGs + CSV tables per `docling`'s always-on TableFormer) persisted for Obsidian-style vault exports; `--no-note` skips the Zotero post entirely. Identical re-runs Skip; PDF updates PATCH-in-place so the note key stays stable. `--delete` is the surgical undo — matches notes by their embedded sentinel (not tag) and trashes them via `sci zot item delete`'s standard path. Requires `docling` on PATH (`sci doctor` installs it via `uv`).

**Library export details.** `sci zot export` honors user-pinned cite-keys (Zotero 7's native `citationKey` field, or legacy Better BibTeX `Citation Key:` lines in `extra`) and synthesizes semantic keys for everything else as `lastname{year}{firstword}-ZOTKEY`. The trailing 8-char Zotero key suffix guarantees uniqueness without collision arithmetic and keeps entries round-trippable back to the source item. Pinned entries also carry a `zotero://select/library/items/<KEY>` URI in the `note` field (appended to any existing user prose, never overwriting). A `.zotero-citekeymap.json` sidecar is written next to the output file; on the next run, any synthesized prefix that drifted (e.g. after a metadata typo fix) gets a biblatex `ids = {oldkey}` alias so manuscripts citing the old form still resolve.

#### Setup & library overview

![zot setup + info](docs/casts/zot-setup.gif)

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

#### Hygiene

![zot doctor](docs/casts/zot-doctor.gif)

</details>

---

## Releases

Every push to `main` triggers the [Release workflow](.github/workflows/release.yml):

1. **Check** — fmt, vet, lint, test
2. **Build** — cross-compiles `sci` and `dbtui` for darwin/linux × arm64/amd64
3. **Publish** — uploads all binaries to a rolling `latest` GitHub release

Binaries are named `{tool}-{os}-{arch}` (e.g. `sci-darwin-arm64`, `dbtui-linux-amd64`).

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
