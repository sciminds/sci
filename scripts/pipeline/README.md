# zot-pipeline — new paper in Zotero → deployed snapshot, unattended

The runner for [the pipeline ticket](../../../obs/vault/Projects/zot/Automate%20the%20pipeline%20from%20new%20paper%20to%20deployed%20snapshot.md). It lives here, in `sci`, because it is an **operate-plane** program: it shells `sci` (which holds the Zotero and OpenAlex credentials) and `zot` (which holds none, and is deployed to an internet-facing VM). The two binaries meet at files in `~/.local/share/zot/staging`, and so does this script — it imports neither and modifies neither repo's code.

| file | what it is |
|---|---|
| `zot-pipeline` | the runner: cadences, gates, lock, ship, state |
| `pipeline-lib.sh` | the pure decision functions — WAL gate, OpenAlex leash, debounce |
| `test-pipeline.sh` | those functions against real sidecars (`just test-pipeline`) |
| `testdata/` | sidecars produced by real `sci zot export` / `openalex sync` runs |
| `com.sciminds.zot-pipeline.fast.plist` | launchd agent: `poll` every 120 s |
| `com.sciminds.zot-pipeline.slow.plist` | launchd agent: `slow` nightly at 03:30 |

## The two cadences

| | fast | slow |
|---|---|---|
| trigger | the Zotero version counter moved, then went quiet | the clock (03:30) |
| stages | export → OpenAlex leash → `zot build` → ship | acquire PDFs → docling → GROBID → `parse-tei` + `parse-docling` → export → leash → build → ship |
| the paper becomes | searchable at `metadata` depth | searchable at `full` depth |

Both take the same lock. Staging has no torn-write protection, so two runners writing it at once is undefined — and `zot build` reads the whole of it.

## Detection — the counter, never the mtime

`zotero.sqlite`'s mtime moves when you open a PDF or add an annotation; the Zotero Web API version counter moves only when the library actually changes. `poll` reads it for both libraries (personal and the sciminds group) using the credentials already in `~/.config/sci/zot.json`, records the move, and waits `DEBOUNCE_SECONDS` (default 300) of quiet before firing — so importing a reading list fires one pipeline, not twenty.

## The freshness gates — three, and the run says which one held

The single *silent* failure in this chain is a dump that parses, counts right, and is missing the papers you added this morning. `sci`'s local reader opens `zotero.sqlite` with `immutable=1`, which is the choice that makes reads available while Zotero desktop holds the file — and the choice that makes committed-but-unflushed changes invisible. Three checks cover the three shapes that gap takes:

- **`wal`** — `pending_wal_bytes` from the export's own sidecar, which is `DB.PendingWAL()` sizing the sibling `-wal`. Nonzero is refused; the dump is discarded and retried, bounded by `GATE_ATTEMPTS × GATE_WAIT_SECONDS`.
- **`journal`** — a hot `zotero.sqlite-journal` beside the database. An `immutable=1` reader skips journal playback exactly as it skips WAL replay. **This is the gate that carries mbp**: mbp's Zotero runs in rollback-journal mode (header byte 18 = `1`), so `PendingWAL` makes no claim there and the WAL gate reads clean no matter what. air's Zotero *is* in WAL mode (byte 18 = `2`), and a Zotero upgrade can flip either machine without telling anyone, so both gates stay.
- **`sync`** — the local mirror has actually caught up to the counter that triggered the run. The counter says the *cloud* moved; it says nothing about the desktop having pulled it. Without this check the fast path would faithfully rebuild from a mirror predating the paper that woke it. Satisfied when the dump carries an item at or beyond the polled version, or — for deletions and collection edits, which no item version can express — when Zotero's own `lastsync` postdates the moment we first saw the counter move.

A dump is written to `staging/.incoming/` and **promoted only after every gate passes**, body first and sidecar last. A rejected dump leaves the last good one exactly where it was.

## The metered stage

`sci zot openalex sync` is the only stage that costs money, and it has no `--limit`: it re-fetches the whole library and then expands every work's `referenced_works`. Its own sidecars measure that at **857 + ceil(167 665 / 50) = 4211 requests**. Two independent questions gate it:

- **worth running?** did the delta introduce a DOI the works cache does not hold — counting only items modified *since* the cache was produced, because ~40 DOIs in this library are permanently unresolvable (OpenAlex 404s on monographs) and would otherwise claim there is work to do forever.
- **allowed to?** does the measured cost fit inside `OA_REQUEST_CAP` (default **50**)?

Under the default cap the second answer is no, every time, and the run says so and records a deferral. **That is the intended behaviour.** The fast path still lands the paper at `metadata` depth; resolving it to an OpenAlex work waits for a human to spend the money — `sci zot --library all openalex sync`, or a cap raised deliberately in the config. An automation that quietly bills is worse than one that quietly fails.

## Shipping

`rsync -a --delete` of the whole `~/.local/share/zot/` artifact directory, minus `staging/`, `quarantine/` and `*.building`. Directory-as-unit is deliberate: a sidecar that lands beside the snapshot tomorrow ships with it and nobody edits this script. rsync's default temp-file-then-rename gives the ship the same atomicity `zot build` gives the build — which is why `--inplace` must never appear here.

The post-ship check runs `zot status --json` on the VM and compares the snapshot version. **`~/.local/bin` is not on the VM's non-interactive ssh `PATH`**, so the remote command exports it explicitly; without that the verification fails with `zot: command not found` and reads as a broken deploy.

## Failure visibility

A failed stage leaves the previous snapshot serving by construction — `zot build` renames over its target, rsync renames over its own. That it *failed* surfaces three ways, cheapest first:

1. `~/.local/state/zot-pipeline/FAILED` — a file whose existence is the alarm, removed by the next success.
2. `zot-pipeline status` — last run, which stage, why, the leash's last decision, what was last shipped.
3. a macOS user notification via `osascript`, best-effort (a launchd agent in the Aqua session can post one; a run over ssh cannot, and the pipeline must not fail because its alarm did).

## Configuration

Defaults live in the script. Override in `~/.config/zot-pipeline/config.env` (sourced if present) or by environment variable:

```sh
OA_REQUEST_CAP=50           # hard ceiling on OpenAlex requests per run
DEBOUNCE_SECONDS=300        # quiet period after the last counter move
GATE_ATTEMPTS=5             # export retries while a freshness gate holds
GATE_WAIT_SECONDS=30
WAL_TOLERANCE_BYTES=0       # nonzero pending WAL is never acceptable
SHIP_HOST=obsmcp.exe.xyz
LIBRARY=all
```

`ZOT_BIN` and `SCI_BIN` override tool resolution. `zot` is deliberately not required on `PATH`: the binary lives in the synced `zotero-mcp` checkout, so it is already on mbp without anything being installed there.

## Installing on mbp

The plists are installed into `~/Library/LaunchAgents/` **unloaded**. Loading them is a deliberate act, taken after a supervised first run.

`zot-pipeline preflight` on mbp currently names two prerequisites, and both are one command:

**1. mbp has no staging directory.** The build is a pure function of staging and mbp has never held one — `sci` and `zot` both ran on air. Seed it once from air (~2.7 GB):

```sh
# on air
rsync -a --stats ~/.local/share/zot/staging/ mbp:.local/share/zot/staging/
```

After that the runner owns the two files it produces (`zotero-items`, and `openalex-works` when the leash lets it) and the two `zot parse-*` produce; the bootstrap OpenAlex caches (`titles`, `authors`, `venues`, `institutions`) are copied inputs with no producing verb and stay as seeded.

**2. mbp does not trust the VM's host key.** The ssh *key* is already right — air and mbp carry the same `id_ed25519`, and it authenticates to `obsmcp.exe.xyz` today. Only `known_hosts` is empty of it, which is what makes the ship step fail with `Host key verification failed`:

```sh
# on mbp
ssh-keyscan -H obsmcp.exe.xyz >> ~/.ssh/known_hosts
ssh -o BatchMode=yes obsmcp.exe.xyz 'export PATH=$HOME/.local/bin:$PATH; zot status'
```

Then the supervised sequence:

```sh
zot-pipeline preflight                              # every prerequisite, named
zot-pipeline --dry-run fast                         # what it would do, and nothing else
zot-pipeline --once --no-ship --no-openalex fast    # a real run: export, gates, build. No spend, no deploy.
zot-pipeline status
zot-pipeline --once --no-openalex fast              # add the ship, verify the VM serves the new version
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.sciminds.zot-pipeline.fast.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.sciminds.zot-pipeline.slow.plist
```

The GROBID arm additionally needs its service up; the runner skips it and says so when it is not, which is why the slow cadence can be loaded before anyone decides how that JVM should be supervised.

The runner is invoked from its synced path (`/Users/esh/syncthing/air-mbp/sci/scripts/pipeline/zot-pipeline`), so editing it on air deploys it to mbp — and the `zot` binary reaches mbp the same way, which is why nothing had to be installed there. The flip side of both: save whole, and expect syncthing latency between building on air and mbp seeing the new binary.
