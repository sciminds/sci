package board

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeObjectStore is an in-memory ObjectStore for tests.
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	// putErr, if set, causes the next PutObject call to fail. Consumed on use.
	putErr error
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string][]byte)}
}

func (f *fakeObjectStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		err := f.putErr
		f.putErr = nil
		return err
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	f.objects[key] = cp
	return nil
}

func (f *fakeObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, nil
}

func (f *fakeObjectStore) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeObjectStore) ListObjects(_ context.Context, prefix, startAfter string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if startAfter != "" && k <= startAfter {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeObjectStore) ListCommonPrefixes(_ context.Context, prefix, delimiter string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	for k := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		idx := strings.Index(rest, delimiter)
		if idx < 0 {
			continue
		}
		seen[prefix+rest[:idx+len(delimiter)]] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// newTestStore builds a Store wired to an in-memory object store and a
// temp-dir SQLite cache, with deterministic now() and sequential event IDs.
func newTestStore(t *testing.T) (*Store, *fakeObjectStore) {
	t.Helper()
	obj := newFakeObjectStore()
	cache, err := OpenLocalCache(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	s := NewStore(obj, cache, "esh")
	clock := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	counter := 0
	s.newID = func() string {
		counter++
		return fmt.Sprintf("0000000000000000000-%016d", counter)
	}
	return s, obj
}

func TestStoreCreateAndListBoards(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	cols := []Column{{ID: "c1", Title: "Todo"}, {ID: "c2", Title: "Done"}}
	if err := s.CreateBoard(ctx, "research", "Research", "", cols); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBoard(ctx, "ops", "Ops", "", cols); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListBoards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "ops" || ids[1] != "research" {
		t.Errorf("ListBoards = %v", ids)
	}
}

func TestStoreCreateBoardDuplicateFails(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "B", "", nil)
	err := s.CreateBoard(ctx, "b", "B", "", nil)
	if err == nil {
		t.Error("expected duplicate error")
	}
}

func TestStoreLoadEmptyBoard(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	cols := []Column{{ID: "c1", Title: "Todo"}}
	_ = s.CreateBoard(ctx, "b", "Test", "desc", cols)

	b, err := s.Load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Test" || len(b.Columns) != 1 || len(b.Cards) != 0 {
		t.Errorf("board: %+v", b)
	}
}

func TestStoreLoadNonexistent(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Load(context.Background(), "ghost")
	if !errors.Is(err, ErrBoardNotFound) {
		t.Errorf("want ErrBoardNotFound, got %v", err)
	}
}

func TestStoreAppendAndLoad(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "Test", "", []Column{{ID: "c1", Title: "Todo"}})

	_, err := s.Append(ctx, "b", OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "Write intro", Column: "c1", Position: 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}

	b, err := s.Load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Cards) != 1 || b.Cards[0].Title != "Write intro" {
		t.Errorf("cards: %+v", b.Cards)
	}
	if b.Cards[0].CreatedBy != "esh" {
		t.Errorf("author: %q", b.Cards[0].CreatedBy)
	}
}

func TestStoreAppendGranularPatches(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})
	_, _ = s.Append(ctx, "b", OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "orig", Description: "origdesc", Column: "c1", Position: 1.0},
	})
	newTitle := "new title"
	newDesc := "new desc"
	_, _ = s.Append(ctx, "b", OpCardPatch, CardPatchPayload{ID: "k1", Title: &newTitle})
	_, _ = s.Append(ctx, "b", OpCardPatch, CardPatchPayload{ID: "k1", Description: &newDesc})

	b, _ := s.Load(ctx, "b")
	if b.Cards[0].Title != "new title" || b.Cards[0].Description != "new desc" {
		t.Errorf("granular patches lost: %+v", b.Cards[0])
	}
}

func TestStoreAppendQueuesOnUploadFailure(t *testing.T) {
	s, obj := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})

	obj.putErr = errors.New("network down")
	_, err := s.Append(ctx, "b", OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Column: "c1", Position: 1.0},
	})
	if err == nil {
		t.Fatal("expected upload error")
	}

	// Event is still durable locally.
	pending, _ := s.local.PendingEvents(ctx, "b")
	if len(pending) != 1 {
		t.Errorf("pending: %d", len(pending))
	}

	// Load must show the optimistic local edit despite not being on R2.
	b, _ := s.Load(ctx, "b")
	if len(b.Cards) != 1 {
		t.Errorf("optimistic load missing pending event: %+v", b.Cards)
	}

	// Flush succeeds once the network comes back.
	if err := s.FlushPending(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	pending, _ = s.local.PendingEvents(ctx, "b")
	if len(pending) != 0 {
		t.Errorf("still pending after flush: %d", len(pending))
	}
}

func TestStorePoll(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})

	e1, _ := s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})
	e2, _ := s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "c1", Position: 2.0}})

	ids, err := s.Poll(ctx, "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("poll all: %v", ids)
	}

	ids, err = s.Poll(ctx, "b", e1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != e2.ID {
		t.Errorf("poll since e1: got %v want [%s]", ids, e2.ID)
	}

	ids, _ = s.Poll(ctx, "b", e2.ID)
	if len(ids) != 0 {
		t.Errorf("poll since latest: %v", ids)
	}
}

func TestStoreSnapshotRoundTrip(t *testing.T) {
	s, obj := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})
	_, _ = s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Title: "A", Column: "c1", Position: 1.0}})
	_, _ = s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Title: "B", Column: "c1", Position: 2.0}})

	b, _ := s.Load(ctx, "b")
	if err := s.Snapshot(ctx, "b", b); err != nil {
		t.Fatal(err)
	}

	// Confirm pointer + snapshot object exist.
	if _, err := obj.GetObject(ctx, snapLatestKey("b")); err != nil {
		t.Errorf("pointer missing: %v", err)
	}

	// Subsequent Load uses the snapshot and still returns the same state.
	b2, err := s.Load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(b2.Cards) != 2 {
		t.Errorf("snapshot-based load: %+v", b2.Cards)
	}

	// Appending after snapshot still works — the new event is folded on top.
	_, _ = s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k3", Title: "C", Column: "c1", Position: 3.0}})
	b3, _ := s.Load(ctx, "b")
	if len(b3.Cards) != 3 {
		t.Errorf("post-snapshot append: %+v", b3.Cards)
	}
}

func TestStoreDeleteBoard(t *testing.T) {
	s, obj := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})
	_, _ = s.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})

	if err := s.DeleteBoard(ctx, "b"); err != nil {
		t.Fatal(err)
	}

	// Every object under boards/b/ should be gone.
	keys, _ := obj.ListObjects(ctx, "boards/b/", "")
	if len(keys) != 0 {
		t.Errorf("leftover keys: %v", keys)
	}

	_, err := s.Load(ctx, "b")
	if !errors.Is(err, ErrBoardNotFound) {
		t.Errorf("load after delete: %v", err)
	}
}

func TestStoreDeterministicFoldAcrossAuthors(t *testing.T) {
	// Two clients writing to the same board produce the same Load result.
	obj := newFakeObjectStore()

	aliceCache, _ := OpenLocalCache(filepath.Join(t.TempDir(), "alice.db"))
	defer func() { _ = aliceCache.Close() }()
	bobCache, _ := OpenLocalCache(filepath.Join(t.TempDir(), "bob.db"))
	defer func() { _ = bobCache.Close() }()

	alice := NewStore(obj, aliceCache, "alice")
	bob := NewStore(obj, bobCache, "bob")
	// Simulate a perfectly synchronized clock — both clients draw IDs from a
	// single monotonic sequence. Real ULIDs include wall-clock time, so a
	// later edit always has a strictly higher ID under normal clock skew.
	var seq int
	sharedID := func() string {
		seq++
		return fmt.Sprintf("%020d", seq)
	}
	alice.newID = sharedID
	bob.newID = sharedID

	ctx := context.Background()
	_ = alice.CreateBoard(ctx, "b", "T", "", []Column{{ID: "c1", Title: "Todo"}})
	_, _ = alice.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "c1", Position: 1.0}})
	_, _ = bob.Append(ctx, "b", OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "c1", Position: 2.0}})
	newTitle := "patched"
	_, _ = alice.Append(ctx, "b", OpCardPatch, CardPatchPayload{ID: "k2", Title: &newTitle})

	ba, err := alice.Load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	bb, err := bob.Load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if !boardsEqual(ba, bb) {
		t.Errorf("alice and bob disagree:\nalice: %+v\nbob:   %+v", ba.Cards, bb.Cards)
	}
	if len(ba.Cards) != 2 {
		t.Errorf("cards: %+v", ba.Cards)
	}
	// Find k2 and check the patch landed.
	for _, c := range ba.Cards {
		if c.ID == "k2" && c.Title != "patched" {
			t.Errorf("cross-author patch lost: %+v", c)
		}
	}
}
