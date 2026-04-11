# CLAUDE.md — internal/board/

Shared kanban board synchronized across authenticated sci lab users via a
private Cloudflare R2 bucket (`sci-board`). This package is the headless
sync/data engine. The TUI lives under `internal/tui/board/` — see that
package's CLAUDE.md for architecture.

## What it is

An append-only event log per board, stored as individual JSON objects in
R2. State is reconstructed by listing all events and folding them in
time-sortable ID order via a pure `Apply` function. Concurrent edits by
different clients merge without conflict because:

1. Each client writes only to its own key namespace → no key collisions.
2. Ops are granular (per-field `card.patch`) → disjoint field edits both survive.
3. Same-field collisions resolve deterministically via ULID (last writer wins).

This design was chosen over single-file JSON w/ CAS and over a real DB to
keep the substrate trivial (R2 PUT/GET/LIST) and avoid any conflict
resolution code. See git history on `8152697` for the original plan.

## File layout

```
doc.go              package overview
position.go         fractional indexing helpers (Between, NeedsNormalize, Normalize)
board.go            Board, BoardMeta, Column, Card, ChecklistItem, Comment value types
event.go            Event + Op constants + 14 typed payload structs + Encode/DecodePayload
apply.go            pure fold: Apply(Board, Event) (Board, error)
local.go            LocalCache — SQLite cache of downloaded events + pending-write queue
store.go            Store — high-level API on top of ObjectStore + LocalCache
cloud_adapter.go    CloudAdapter — satisfies ObjectStore using cloud.Client's raw *s3.Client
live_test.go        BOARD_LIVE=1-gated R2 smoke tests (round trip + privacy assertion)
```

## Data model

### BoardMeta (non-card metadata, stored in `boards/{id}/meta.json`)
```go
type BoardMeta struct {
    ID, Title, Description string
    Columns     []Column
    CreatedAt, UpdatedAt time.Time
    CreatedBy   string  // github login
}
```

### Board (BoardMeta + Cards — the folded state Apply returns)
```go
type Board struct {
    BoardMeta
    Cards []Card
}
```

### Column
```go
type Column struct {
    ID, Title string
    WIP       int  // 0 = no limit
}
```

### Card
```go
type Card struct {
    ID, Title          string
    Description        string     // markdown, expected to be up to ~a page
    Column             string     // column ID this card belongs to
    Position           float64    // fractional index within column; see position.go
    Priority           string     // "", "low", "med", "high"
    Labels, Assignees  []string
    DueDate            *time.Time // soft target
    Deadline           *time.Time // hard cutoff
    Checklist          []ChecklistItem
    Comments           []Comment  // append-only; no edit/delete ops
    CreatedAt, UpdatedAt time.Time
    CreatedBy, UpdatedBy string
}

type ChecklistItem struct { ID, Text string; Done bool }
type Comment       struct { ID, Author, Text string; Ts time.Time }
```

**Two date fields are intentional:** `DueDate` is soft, `Deadline` is hard.
UI should render them differently (suggest `○` for due, `◆` for deadline).
Either may be nil. Clearing is not supported in v1 — patch with non-nil
sets; no API to un-set. Add if needed.

### Card ordering within a column

Cards in a column are sorted by `Position` (a float64 > 0). Inserting
between two cards uses `Between(left, right) float64` — returns the
midpoint. Inserting at start: `Between(0, first)`. At end: `Between(last, 0)`.
Empty list: `Between(0, 0)` → 1.0.

After ~10^9 midpoint insertions at the same spot, precision runs out.
`NeedsNormalize(positions) bool` detects this. `Normalize(positions)`
renumbers to 1.0, 2.0, 3.0, …. v1 does not auto-emit normalization ops —
positions drift quietly. If this becomes a problem, emit a
`column.reorder`-like op that carries new positions for affected cards.

## Events

Every mutation is an `Event`:

```go
type Event struct {
    ID      string           // time-sortable unique ID; see newEventID in store.go
    Board   string
    Author  string           // github login
    Ts      time.Time
    Op      Op
    Payload json.RawMessage  // op-specific, one typed struct per Op
}
```

### The 15 ops

| Op | Payload | Notes |
|---|---|---|
| `board.create` | `BoardCreatePayload{Title, Description, Columns}` | Idempotent — first event on a board |
| `board.update` | `BoardUpdatePayload{Title*, Description*}` | Partial, nil fields unchanged |
| `board.delete` | `BoardDeletePayload{}` | No-op in Apply — Store.DeleteBoard handles whole-prefix removal |
| `column.add` | `ColumnAddPayload{Column}` | Duplicate ID is no-op |
| `column.rename` | `ColumnRenamePayload{ID, Title, WIP*}` | |
| `column.reorder` | `ColumnReorderPayload{ColumnIDs []string}` | Full ordering; missing IDs dropped, unlisted IDs appended |
| `column.delete` | `ColumnDeletePayload{ID}` | Cards in deleted column are orphaned (UI hides), not removed |
| `card.add` | `CardAddPayload{Card}` | Duplicate ID is no-op |
| `card.patch` | `CardPatchPayload{ID, Title*, Description*, Priority*, Labels*, Assignees*, DueDate*, Deadline*}` | Per-field partial update — concurrent patches to disjoint fields both survive |
| `card.move` | `CardMovePayload{ID, Column, Position}` | |
| `card.delete` | `CardDeletePayload{ID}` | Subsequent ops on deleted card are no-ops, not errors |
| `card.comment.add` | `CommentAddPayload{CardID, Comment}` | Append-only — no edit or delete op for comments |
| `card.checklist.add` | `ChecklistAddPayload{CardID, Item}` | |
| `card.checklist.toggle` | `ChecklistTogglePayload{CardID, ItemID}` | Flips Done |
| `card.checklist.delete` | `ChecklistDeletePayload{CardID, ItemID}` | |

`DecodePayload(e Event) (any, error)` returns the typed payload based on
`e.Op`. Unknown ops return `*UnknownOpError` (forward-compat: new clients
can add ops older clients ignore).

### Apply semantics

```go
func Apply(b Board, e Event) (Board, error)
```

**Pure function.** Same (board, event) pair always produces the same
result. Never mutates the input — returns a new Board with copied slices.

**Error semantics:**
- Malformed payload → error; callers should log and skip.
- Well-formed event targeting a missing card/column → no-op, not an error.
  The event log is immutable history, so old ops may legitimately reference
  later-deleted targets.
- `board.delete` is a no-op here; whole-board removal is handled by
  `Store.DeleteBoard` (removes R2 prefix directly).

**Determinism invariant:** `TestApplyDeterminism` shuffles a fixture event
list 20 times, sorts by ID, and folds; all runs must produce byte-identical
boards. This is the property that lets concurrent clients converge.

## Key layout on R2

Bucket: `sci-board` (private; verified by `TestLiveBoardBucketIsPrivate`).

```
boards/{id}/meta.json                 ← BoardMeta (small, rarely updated)
boards/{id}/snap/latest.json          ← SnapshotPointer{Key, UpToID, SavedAt, SavedBy}
boards/{id}/snap/{eventID}.json       ← full Board snapshot (immutable)
boards/{id}/events/{eventID}.json     ← one Event per file
```

Helpers in `store.go`: `boardPrefix`, `metaKey`, `snapLatestKey`, `snapKey`,
`eventsPrefix`, `eventKey`.

**No per-user key subpath.** The earlier design had `events/{author}/{ulid}.json`
for a "conflict-free by key partitioning" story, but we dropped it because
ULIDs are globally unique by construction (time + 8 random bytes, see
`newEventID`) and per-user subpaths broke clean `LIST` with `StartAfter`.
Author is carried in the event body.

**Board enumeration** uses `LIST boards/` with `delimiter=/` and reads the
`CommonPrefixes` response. No index file to keep in sync.

**Only contended key:** `snap/latest.json`. Two clients racing to update
the snapshot pointer both write valid pointers — losing the race is
harmless because any valid pointer leads to a fold-equivalent state.

## Event IDs — critical invariant

`newEventID()` returns `{unix-nanos zero-padded 19 digits}-{16 hex chars}`.
Two guarantees matter:

1. **Lexicographically sortable by time.** Sorting events by ID = sorting
   by wall-clock time (modulo clock skew between clients). This is what
   makes `Apply` deterministic across clients.
2. **Globally unique.** 64 bits of crypto/rand suffix → collisions
   astronomically unlikely even with all 12 lab users writing simultaneously.

**Failure mode to be aware of:** large clock skew can cause out-of-order
folds. Example: Alice's patch-at-12:00 has an older ID than Bob's
add-at-11:59:59 if Alice's clock is way behind. Apply would see the patch
first, target-not-found, no-op, drop the edit. Normal NTP-synced clocks
(skew < 1s) make this vanishingly unlikely. The test `TestStoreDeterministicFoldAcrossAuthors`
pins the assumption.

## Store API

```go
func NewStore(obj ObjectStore, local *LocalCache, author string) *Store

func (s *Store) CreateBoard(ctx, id, title, description string, columns []Column) error
func (s *Store) DeleteBoard(ctx, id string) error
func (s *Store) ListBoards(ctx) ([]string, error)
func (s *Store) Load(ctx, boardID string) (Board, error)
func (s *Store) Append(ctx, boardID string, op Op, payload any) (Event, error)
func (s *Store) FlushPending(ctx, boardID string) error
func (s *Store) Poll(ctx, boardID, sinceID string) ([]string, error)  // new event IDs, no body download
func (s *Store) Snapshot(ctx, boardID string, b Board) error
```

### Load

1. Try `GET snap/latest.json` → if present, load the referenced snapshot as the starting state
2. Otherwise `GET meta.json` → bootstrap Board from meta only
3. `LIST events/` with `start-after = eventKey(sinceID)` for events newer than the snapshot
4. Parallel `GET` each event, sort by ID, fold via `Apply`
5. **Apply pending local events on top** — events the user has queued locally but not yet uploaded. Critical for optimistic UI.
6. Cache downloaded events + update `sync_state` + cache BoardMeta
7. Return the folded Board

### Append (optimistic, durable)

1. Build Event with fresh ID, author, timestamp
2. `LocalCache.QueuePending` — durable even if next step fails
3. `ObjectStore.PutObject` to R2
4. On success: `RemovePending` + `CacheEvents` (move from pending → cached)
5. On failure: event stays in pending queue, error returned to caller

**The caller's optimistic UI should apply the returned Event locally
regardless of error.** Pending events are reapplied on every `Load`, so
offline edits survive across restarts.

### FlushPending

Retries all queued events for a board. Called on startup or when
reconnectivity is detected. Stops at first failure to preserve queue order.

### Poll

Cheap — just a LIST, no body downloads. Returns IDs only. TUI should call
this every ~30s while open and show a toast ("N updates from @alice — press
R to reload") when non-empty. Polling is the entire "live updates" story —
no live merge, user opts in to reload.

### Snapshot

Called when `Load` observes too many events past the last snapshot (e.g.,
>200). Any client can snapshot; race-safe. v1 does not auto-trigger;
compaction is tracked as step 13 of the rollout plan.

## LocalCache

`~/.local/state/sci/board.db` (path chosen by caller; tests use `t.TempDir`).
Raw `database/sql` + `modernc.org/sqlite` (no pocketbase/dbx — follows
dbtui/markdb precedent for TUI-adjacent packages).

Tables:
- `events_cached` — all downloaded events, PK `(board_id, event_id)`, idempotent inserts
- `events_pending` — queued-but-unsent events, ordered by AUTOINCREMENT rowid
- `sync_state` — per-board `{last_seen_event_id, last_snapshot_key, last_snapshot_up_to}`
- `boards_meta` — BoardMeta per board

Everything is per-board — one SQLite file holds caches for N boards. Cross-
board isolation is enforced by WHERE clauses.

## ObjectStore interface + CloudAdapter

`Store` depends on an `ObjectStore` interface (5 methods: Put/Get/Delete,
ListObjects with start-after, ListCommonPrefixes). Unit tests use an
in-memory fake (`fakeObjectStore` in `store_test.go`). Production uses
`CloudAdapter` which wraps `*cloud.Client` and talks to `client.S3`
directly, bypassing `cloud.Client.Upload`/`Download`'s username-prefix
auto-insertion.

`cloud.Client`'s Upload/Download/Delete/ListPrefix auto-prepend `{username}/`
to the key via `objectKey`. That is the right thing for `sci cloud put`
but wrong for shared board data — everyone writes into the same key
namespace. `CloudAdapter` uses the raw `*s3.Client` with full keys.

## Auth flow (existing-user migration)

`cloud.LoadConfig` backfills `cfg.Board` from `cfg.Public` when only the
legacy public block is present. R2 tokens are account-scoped in our setup,
so the same keys work for both buckets. Users upgrading via `sci update`
get the board feature transparently — no re-auth required. See
`TestLoadConfig_LegacyMigration` and `TestLoadConfig_NewFormat`.

Bucket name is hardcoded as `defaultBoardBucket = "sci-board"` in
`internal/cloud/auth.go`, matching the worker's `BOARD_BUCKET_NAME` env var.

## Testing

- Unit tests are fully in-memory (fake ObjectStore, temp SQLite). `just ok`
  runs them every time — 59 tests across the package.
- Live tests (`live_test.go`) are gated on `BOARD_LIVE=1`:
  ```
  BOARD_LIVE=1 go test ./internal/board/ -v
  ```
  Two tests: round-trip smoke against real R2, and a privacy assertion that
  the `sci-board` bucket refuses unauthenticated reads.

## Gotchas

- **Event IDs MUST be time-sortable across clients.** Fake IDs in tests must
  preserve insertion order or cross-author tests will see dropped patches.
  See `TestStoreDeterministicFoldAcrossAuthors` for the pattern (shared
  monotonic counter simulates synced clocks).
- **Apply is pure.** Never mutate `b` in place; always return a new Board
  with new slices. `append([]Card(nil), b.Cards...)` before edits.
- **`card.patch` semantics:** nil `*string` / nil `*[]string` mean "no
  change". There is no way to clear a field (no null distinction from
  absent). If needed, add explicit `ClearXxx` bools or a new op.
- **Comments are append-only by design.** No edit/delete ops. If users
  need correction, that's a `card.comment.add` with "correction: …".
- **Orphaned cards after `column.delete`** are left with their `Column`
  field intact. The UI is expected to hide cards whose column no longer
  exists. A later `card.move` can rescue them.
- **Don't use `cloud.Client.Upload` for board events** — it prefixes the
  key with `{username}/`. Always go through `CloudAdapter`.
- **`card.move` doesn't preserve timestamps on reorder churn.** Moving a
  card updates `UpdatedAt` even if just repositioning within the same
  column. Consider whether to split `card.move` into move-column vs
  reorder-within-column if this is noisy.

## Consumers

- **`internal/tui/board/`** — the bubbletea TUI. Uses `Store.ListBoards`,
  `Store.Load`, `Store.Poll`, and (not yet wired for edits) `Store.Append`.
- **`cmd/board.go`** — not yet built. When added, it is the place to wire
  `cloud.SetupBoard()` → `NewCloudAdapter` → `OpenLocalCache` → `NewStore`
  and launch the TUI.
