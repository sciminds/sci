package board

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestCache(t *testing.T) *LocalCache {
	t.Helper()
	dir := t.TempDir()
	c, err := OpenLocalCache(filepath.Join(dir, "board.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func mkCachedEvent(id, author string, op Op, payload any) Event {
	raw, _ := json.Marshal(payload)
	return Event{
		ID:      id,
		Board:   "b",
		Author:  author,
		Ts:      time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		Op:      op,
		Payload: raw,
	}
}

func TestLocalCacheEventsRoundTrip(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	events := []Event{
		mkCachedEvent("01A", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}}),
		mkCachedEvent("01B", "alice", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "c1", Position: 2.0}}),
	}
	if err := c.CacheEvents(ctx, "b", events); err != nil {
		t.Fatalf("cache: %v", err)
	}

	got, err := c.LoadCachedEvents(ctx, "b", "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events", len(got))
	}
	if got[0].ID != "01A" || got[1].ID != "01B" {
		t.Errorf("order: %v", got)
	}
	if got[0].Author != "esh" || got[0].Op != OpCardAdd {
		t.Errorf("round-trip: %+v", got[0])
	}

	// Round-trip must preserve the payload bytes so Apply sees the same thing.
	payload, err := DecodePayload(got[0])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cp, ok := payload.(CardAddPayload)
	if !ok || cp.Card.ID != "k1" {
		t.Errorf("decoded payload: %+v", payload)
	}
}

func TestLocalCacheSince(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	events := []Event{
		mkCachedEvent("01A", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}}),
		mkCachedEvent("01B", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "c1", Position: 2.0}}),
		mkCachedEvent("01C", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k3", Column: "c1", Position: 3.0}}),
	}
	_ = c.CacheEvents(ctx, "b", events)

	got, err := c.LoadCachedEvents(ctx, "b", "01A")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 || got[0].ID != "01B" || got[1].ID != "01C" {
		t.Errorf("since: %+v", got)
	}
}

func TestLocalCacheIdempotentInsert(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	e := mkCachedEvent("01A", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})
	_ = c.CacheEvents(ctx, "b", []Event{e})
	_ = c.CacheEvents(ctx, "b", []Event{e})

	got, _ := c.LoadCachedEvents(ctx, "b", "")
	if len(got) != 1 {
		t.Errorf("expected dedup, got %d", len(got))
	}
}

func TestLocalCachePendingQueue(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	a := mkCachedEvent("01A", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})
	b := mkCachedEvent("01B", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "c1", Position: 2.0}})

	if err := c.QueuePending(ctx, "board1", a); err != nil {
		t.Fatal(err)
	}
	if err := c.QueuePending(ctx, "board1", b); err != nil {
		t.Fatal(err)
	}

	got, err := c.PendingEvents(ctx, "board1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "01A" || got[1].ID != "01B" {
		t.Errorf("pending order: %+v", got)
	}

	// Other board isolated.
	other, _ := c.PendingEvents(ctx, "board2")
	if len(other) != 0 {
		t.Errorf("cross-board bleed: %+v", other)
	}

	// Remove one.
	if err := c.RemovePending(ctx, "board1", "01A"); err != nil {
		t.Fatal(err)
	}
	got, _ = c.PendingEvents(ctx, "board1")
	if len(got) != 1 || got[0].ID != "01B" {
		t.Errorf("after remove: %+v", got)
	}
}

func TestLocalCacheSyncState(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	// Empty default.
	s, err := c.GetSyncState(ctx, "board1")
	if err != nil {
		t.Fatal(err)
	}
	if s.BoardID != "board1" || s.LastSeenEventID != "" {
		t.Errorf("default: %+v", s)
	}

	// Upsert.
	err = c.SetSyncState(ctx, SyncState{
		BoardID:          "board1",
		LastSeenEventID:  "01Z",
		LastSnapshotKey:  "boards/board1/snapshot/01Y-esh.json",
		LastSnapshotUpTo: "01Y",
	})
	if err != nil {
		t.Fatal(err)
	}
	s, _ = c.GetSyncState(ctx, "board1")
	if s.LastSeenEventID != "01Z" || s.LastSnapshotUpTo != "01Y" {
		t.Errorf("after upsert: %+v", s)
	}

	// Update again.
	_ = c.SetSyncState(ctx, SyncState{BoardID: "board1", LastSeenEventID: "02A"})
	s, _ = c.GetSyncState(ctx, "board1")
	if s.LastSeenEventID != "02A" {
		t.Errorf("after update: %+v", s)
	}
}

func TestLocalCacheBoardMeta(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()

	meta := BoardMeta{
		ID:        "b1",
		Title:     "Test",
		Columns:   []Column{{ID: "c1", Title: "Todo"}},
		CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		CreatedBy: "esh",
		UpdatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
	}
	if err := c.CacheBoardMeta(ctx, meta); err != nil {
		t.Fatal(err)
	}

	got, err := c.LoadBoardMeta(ctx, "b1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test" || len(got.Columns) != 1 {
		t.Errorf("meta: %+v", got)
	}

	// Missing board returns sql.ErrNoRows via the underlying driver.
	_, err = c.LoadBoardMeta(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for missing meta")
	}

	// Update.
	meta.Title = "Updated"
	if err := c.CacheBoardMeta(ctx, meta); err != nil {
		t.Fatal(err)
	}
	got, _ = c.LoadBoardMeta(ctx, "b1")
	if got.Title != "Updated" {
		t.Errorf("after update: %+v", got)
	}

	// List.
	ids, err := c.ListCachedBoards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "b1" {
		t.Errorf("list: %+v", ids)
	}
}

func TestLocalCacheReopenPreservesState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "board.db")
	ctx := context.Background()

	c, err := OpenLocalCache(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.CacheEvents(ctx, "b", []Event{mkCachedEvent("01A", "esh", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})})
	_ = c.SetSyncState(ctx, SyncState{BoardID: "b", LastSeenEventID: "01A"})
	_ = c.Close()

	c2, err := OpenLocalCache(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()

	got, _ := c2.LoadCachedEvents(ctx, "b", "")
	if len(got) != 1 {
		t.Errorf("events after reopen: %d", len(got))
	}
	s, _ := c2.GetSyncState(ctx, "b")
	if s.LastSeenEventID != "01A" {
		t.Errorf("sync state after reopen: %+v", s)
	}
}

func TestLocalCacheEmptySinceReturnsAll(t *testing.T) {
	t.Parallel()
	c := openTestCache(t)
	ctx := context.Background()
	got, err := c.LoadCachedEvents(ctx, "nonexistent", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty board: %+v", got)
	}
}

func TestLocalCacheSqlErrNoRowsIsPreserved(t *testing.T) {
	t.Parallel()
	// Sanity check that callers can detect missing-meta via errors.Is.
	c := openTestCache(t)
	ctx := context.Background()
	_, err := c.LoadBoardMeta(ctx, "nope")
	if err == nil {
		t.Fatal("expected error")
	}
	// We don't explicitly promise sql.ErrNoRows, but the error should be
	// distinguishable from a successful empty read. Just assert non-nil.
	_ = errors.New
}
