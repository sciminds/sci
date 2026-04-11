# CLAUDE.md — internal/board/

Headless sync engine for shared kanban boards. Append-only event log per board, stored as individual JSON objects in a private R2 bucket (`sci-board`). The TUI consumer lives under `internal/tui/board/`.

For type definitions, file layout, and the full op list, read the source — `doc.go`, `board.go`, `event.go`, `apply.go`, `store.go` are the truth.

## Why the design works

State is reconstructed by `LIST events/`, sorting by ID, and folding via the pure `Apply` function. Concurrent edits by different clients converge without conflict resolution code because:

1. **Globally unique event IDs** — `newEventID` returns `{unix-nanos zero-padded 19 digits}-{16 hex chars from crypto/rand}`. No client ever collides with another.
2. **Lex-sortable IDs == time-sortable.** Sorting by ID = sorting by wall clock (modulo skew). This is what makes `Apply` deterministic across clients. `TestApplyDeterminism` shuffles fixture events 20× and asserts byte-identical output — that property is load-bearing.
3. **Granular ops.** `card.patch` is per-field, so disjoint field edits both survive. Same-field collisions resolve last-writer-wins via ULID order.

Chosen over single-file JSON + CAS and over a real DB to keep the substrate trivial (R2 PUT/GET/LIST). Original plan in commit `8152697`.

## R2 key layout (load-bearing — wrong here = data corruption)

Bucket: `sci-board` (private; verified by `TestLiveBoardBucketIsPrivate`).

```
boards/{id}/meta.json                 ← BoardMeta (small, rarely updated)
boards/{id}/snap/latest.json          ← SnapshotPointer
boards/{id}/snap/{eventID}.json       ← full Board snapshot (immutable)
boards/{id}/events/{eventID}.json     ← one Event per file
```

- **No per-user key subpath.** ULIDs are already globally unique; per-user subpaths broke clean `LIST` with `StartAfter`. Author is in the event body.
- **Board enumeration** uses `LIST boards/` with `delimiter=/` and reads `CommonPrefixes`. No index file to keep in sync.
- **Only contended key:** `snap/latest.json`. Two clients racing both write valid pointers — losing the race is harmless because any valid pointer leads to a fold-equivalent state.

## Apply semantics

- **Pure.** Same `(board, event)` always produces the same result. Never mutate `b` in place; always return a new Board with new slices (`append([]Card(nil), b.Cards...)` before edits).
- **Malformed payload** → error; callers log and skip.
- **Well-formed event targeting a missing card/column** → no-op, not an error. The event log is immutable history and old ops may legitimately reference later-deleted targets.
- **`board.delete`** is a no-op in Apply; whole-board removal is `Store.DeleteBoard` (removes the R2 prefix directly).

## Store flows

- **Load.** Try `snap/latest.json` first; otherwise bootstrap from `meta.json`. Then `LIST events/` with `start-after = eventKey(sinceID)`, parallel GET, sort by ID, fold. Critically: **apply pending local events on top** — events queued locally but not yet uploaded — so optimistic UI survives across restarts.
- **Append (optimistic, durable).** Build event → `LocalCache.QueuePending` (durable) → PUT to R2 → on success move from pending to cached, on failure leave in queue. The caller's optimistic UI applies the returned event regardless of error.
- **FlushPending.** Retries the queue in order; stops at first failure to preserve ordering.
- **Poll.** Cheap LIST, no body downloads, returns IDs only. TUI polls every ~30s and surfaces a toast — no live merge, user opts in to reload.
- **Snapshot.** Race-safe; any client may snapshot. v1 doesn't auto-trigger.

## ObjectStore + CloudAdapter — DO NOT USE `cloud.Client.Upload`

`Store` depends on a 5-method `ObjectStore` interface. Production uses `CloudAdapter`, which wraps `*cloud.Client` and talks to `client.S3` directly.

`cloud.Client.Upload`/`Download`/`Delete`/`ListPrefix` auto-prepend `{username}/` to the key via `objectKey`. That's correct for `sci cloud put` but wrong for shared board data — every user would write into a different namespace and never see each other's events. **Always go through `CloudAdapter`** for board ops.

## Auth migration

`cloud.LoadConfig` backfills `cfg.Board` from `cfg.Public` when only the legacy block is present. R2 tokens are account-scoped, so the same keys work for both buckets. Existing users get the board feature transparently — no re-auth. See `TestLoadConfig_LegacyMigration`. Bucket name hardcoded as `defaultBoardBucket = "sci-board"` in `internal/cloud/auth.go`, matching the worker's `BOARD_BUCKET_NAME` env var.

## Card positions

Cards in a column are sorted by `Position` (float64 > 0). Insert via `Between(left, right)` (midpoint). After ~10⁹ midpoint insertions at the same spot, precision runs out. `NeedsNormalize` detects it; `Normalize` renumbers to 1.0, 2.0, 3.0, …. v1 does not auto-emit normalization — positions drift quietly. If it becomes a problem, add a reorder op.

## Testing

- Unit tests are fully in-memory (fake `ObjectStore`, temp SQLite). `just ok` runs them every time.
- Live R2 tests gated on `BOARD_LIVE=1` — `just test-board-live`.

## Gotchas

- **Event IDs MUST be time-sortable across clients.** Fake IDs in tests must preserve insertion order or cross-author tests will see dropped patches. See `TestStoreDeterministicFoldAcrossAuthors` for the shared-counter pattern.
- **`card.patch` has no clear-field semantics.** nil `*string` / nil `*[]string` mean "no change". There's no null distinction from absent. If clearing is needed, add explicit `ClearXxx` bools or a new op.
- **Comments are append-only by design.** No edit/delete op. Corrections are a new comment.
- **Orphaned cards after `column.delete`** keep their `Column` field intact. UI hides them. A later `card.move` can rescue them.
- **Two date fields** (`DueDate` soft, `Deadline` hard) are intentional. Either may be nil. Clearing not supported in v1.
- **Clock skew** > a few seconds can cause out-of-order folds (Alice's late patch lands before Bob's earlier add → target-not-found → no-op → edit dropped). Normal NTP-synced clocks make this vanishingly unlikely; `TestStoreDeterministicFoldAcrossAuthors` pins the assumption.
- **`card.move` updates `UpdatedAt` on every reorder** even within the same column. If churn becomes noisy, split into move-column vs reorder-within-column.

## Consumers

- `internal/tui/board/` — bubbletea TUI. Uses `ListBoards`, `Load`, `Poll`, and (not yet for edits) `Append`.
- `cmd/board.go` — not yet built. When added, wire `cloud.SetupBoard()` → `NewCloudAdapter` → `OpenLocalCache` → `NewStore` → `tui.board.Run`.
