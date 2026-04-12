package board

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ObjectStore is the minimal interface Store needs from the underlying
// object store (R2, S3, or a fake). The real cloud.Client will implement
// these in step 7 via thin wrappers; tests use an in-memory map.
//
// All keys are full object keys — no auto-prefixing. Store owns key layout.
type ObjectStore interface {
	PutObject(ctx context.Context, key string, body []byte, contentType string) error
	GetObject(ctx context.Context, key string) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
	// ListObjects returns keys under prefix whose key is strictly greater
	// than startAfter. An empty startAfter lists everything under the prefix.
	ListObjects(ctx context.Context, prefix, startAfter string) ([]string, error)
	// ListCommonPrefixes returns the set of "sub-directories" under prefix
	// when split by delimiter — the S3 CommonPrefixes concept. Used to
	// enumerate boards without downloading their contents.
	ListCommonPrefixes(ctx context.Context, prefix, delimiter string) ([]string, error)
}

// ErrBoardNotFound is returned by Load when a board has no meta.json and no
// snapshot on the object store.
var ErrBoardNotFound = errors.New("board: not found")

// SnapshotPointer is the content of snap/latest.json — a tiny JSON blob
// that tells clients which snapshot to fetch and how far it covers.
type SnapshotPointer struct {
	Key     string    `json:"key"`
	UpToID  string    `json:"up_to_event_id"`
	SavedAt time.Time `json:"saved_at"`
	SavedBy string    `json:"saved_by"`
}

// Store is the high-level API used by the CLI and TUI. It glues the
// ObjectStore (remote truth) with LocalCache (offline survival + pending
// write queue) and folds events via Apply.
type Store struct {
	obj    ObjectStore
	local  *LocalCache
	author string
	now    func() time.Time
	newID  func() string
}

// NewStore constructs a Store. author is the GitHub login from the cloud
// auth flow; it is stamped onto every event and snapshot this client writes.
func NewStore(obj ObjectStore, local *LocalCache, author string) *Store {
	return &Store{
		obj:    obj,
		local:  local,
		author: author,
		now:    func() time.Time { return time.Now().UTC() },
		newID:  newEventID,
	}
}

// newEventID generates a time-sortable unique ID. Format: zero-padded 19-digit
// unix nanoseconds, a dash, then 16 hex chars of cryptographic randomness.
// Lexicographic order matches chronological order; collisions are
// astronomically unlikely even across concurrent clients.
func newEventID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%019d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(buf[:]))
}

// Key layout. The bucket itself is "sci-board", so no package-level prefix.
//
//	boards/{id}/meta.json
//	boards/{id}/snap/latest.json         — SnapshotPointer
//	boards/{id}/snap/{ulid}.json         — full Board snapshot
//	boards/{id}/events/{ulid}.json       — one Event per file

func boardPrefix(id string) string       { return "boards/" + id + "/" }
func metaKey(id string) string           { return boardPrefix(id) + "meta.json" }
func snapLatestKey(id string) string     { return boardPrefix(id) + "snap/latest.json" }
func snapKey(id, eventID string) string  { return boardPrefix(id) + "snap/" + eventID + ".json" }
func eventsPrefix(id string) string      { return boardPrefix(id) + "events/" }
func eventKey(id, eventID string) string { return eventsPrefix(id) + eventID + ".json" }

// CreateBoard writes meta.json for a new board. Fails if a board with the
// given ID already exists on the object store.
func (s *Store) CreateBoard(ctx context.Context, id, title, description string, columns []Column) error {
	existing, _ := s.obj.GetObject(ctx, metaKey(id))
	if len(existing) > 0 {
		return fmt.Errorf("board %q already exists", id)
	}
	now := s.now()
	meta := BoardMeta{
		ID:          id,
		Title:       title,
		Description: description,
		Columns:     append([]Column(nil), columns...),
		CreatedAt:   now,
		CreatedBy:   s.author,
		UpdatedAt:   now,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := s.obj.PutObject(ctx, metaKey(id), data, "application/json"); err != nil {
		return err
	}
	return s.local.CacheBoardMeta(ctx, meta)
}

// DeleteBoard removes every object under the board's prefix. This is the
// nuclear option — there is no undo.
func (s *Store) DeleteBoard(ctx context.Context, id string) error {
	keys, err := s.obj.ListObjects(ctx, boardPrefix(id), "")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.obj.DeleteObject(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// ListBoards returns the IDs of all boards on the object store. Discovered
// via LIST with delimiter — no index file to keep in sync.
func (s *Store) ListBoards(ctx context.Context) ([]string, error) {
	prefixes, err := s.obj.ListCommonPrefixes(ctx, "boards/", "/")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		id := strings.TrimPrefix(p, "boards/")
		id = strings.TrimSuffix(id, "/")
		if id != "" {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// Load fetches, folds, and returns the current state of a board. It prefers
// the latest snapshot when one is present, then tails events written after
// the snapshot's upToID. Pending local events are applied on top so the
// caller sees its own un-uploaded edits.
func (s *Store) Load(ctx context.Context, boardID string) (Board, error) {
	var b Board
	var sinceID string

	// Try snapshot.
	if ptrBytes, err := s.obj.GetObject(ctx, snapLatestKey(boardID)); err == nil && len(ptrBytes) > 0 {
		var ptr SnapshotPointer
		if err := json.Unmarshal(ptrBytes, &ptr); err == nil && ptr.Key != "" {
			if snapBytes, err := s.obj.GetObject(ctx, ptr.Key); err == nil {
				var snap Board
				if err := json.Unmarshal(snapBytes, &snap); err == nil {
					b = snap
					sinceID = ptr.UpToID
				}
			}
		}
	}

	// If no snapshot, bootstrap from meta.json.
	if b.ID == "" {
		metaBytes, err := s.obj.GetObject(ctx, metaKey(boardID))
		if err != nil {
			return Board{}, fmt.Errorf("%w: %s", ErrBoardNotFound, boardID)
		}
		var meta BoardMeta
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			return Board{}, fmt.Errorf("parse meta: %w", err)
		}
		b = Board{BoardMeta: meta}
	}

	// List events newer than the snapshot.
	startAfter := ""
	if sinceID != "" {
		startAfter = eventKey(boardID, sinceID)
	}
	keys, err := s.obj.ListObjects(ctx, eventsPrefix(boardID), startAfter)
	if err != nil {
		return b, fmt.Errorf("list events: %w", err)
	}

	events := make([]Event, 0, len(keys))
	for _, k := range keys {
		data, err := s.obj.GetObject(ctx, k)
		if err != nil {
			continue // best effort — missing object just means someone deleted it
		}
		var e Event
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		events = append(events, e)
	}
	slices.SortFunc(events, func(a, b Event) int { return cmp.Compare(a.ID, b.ID) })

	lastID := sinceID
	for _, e := range events {
		next, err := Apply(b, e)
		if err != nil {
			continue // malformed event — skip, log later when we have a logger
		}
		b = next
		if e.ID > lastID {
			lastID = e.ID
		}
	}

	// Apply locally-queued events that haven't been uploaded yet. These are
	// the user's own edits waiting to go out — they must be visible.
	pending, _ := s.local.PendingEvents(ctx, boardID)
	for _, e := range pending {
		if next, err := Apply(b, e); err == nil {
			b = next
		}
	}

	_ = s.local.CacheEvents(ctx, boardID, events)
	_ = s.local.SetSyncState(ctx, SyncState{BoardID: boardID, LastSeenEventID: lastID})
	_ = s.local.CacheBoardMeta(ctx, b.BoardMeta)

	return b, nil
}

// Append creates an event, queues it locally, then attempts to upload it.
// On upload failure the event stays in the pending queue and the returned
// error is non-nil — but the event has been durably recorded locally, and
// the caller's optimistic UI is free to show it.
func (s *Store) Append(ctx context.Context, boardID string, op Op, payload any) (Event, error) {
	raw, err := EncodePayload(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode payload: %w", err)
	}
	e := Event{
		ID:      s.newID(),
		Board:   boardID,
		Author:  s.author,
		Ts:      s.now(),
		Op:      op,
		Payload: raw,
	}
	if err := s.local.QueuePending(ctx, boardID, e); err != nil {
		return e, fmt.Errorf("queue pending: %w", err)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	if err := s.obj.PutObject(ctx, eventKey(boardID, e.ID), data, "application/json"); err != nil {
		return e, fmt.Errorf("upload event: %w", err)
	}

	_ = s.local.RemovePending(ctx, boardID, e.ID)
	_ = s.local.CacheEvents(ctx, boardID, []Event{e})
	return e, nil
}

// FlushPending retries all queued-but-unsent events for a board. It stops
// at the first upload failure so the queue ordering is preserved.
func (s *Store) FlushPending(ctx context.Context, boardID string) error {
	pending, err := s.local.PendingEvents(ctx, boardID)
	if err != nil {
		return err
	}
	for _, e := range pending {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := s.obj.PutObject(ctx, eventKey(boardID, e.ID), data, "application/json"); err != nil {
			return err
		}
		if err := s.local.RemovePending(ctx, boardID, e.ID); err != nil {
			return err
		}
		_ = s.local.CacheEvents(ctx, boardID, []Event{e})
	}
	return nil
}

// Poll returns the IDs of events strictly newer than sinceID, without
// downloading their bodies. The TUI uses this for the "board updated" toast.
func (s *Store) Poll(ctx context.Context, boardID, sinceID string) ([]string, error) {
	startAfter := ""
	if sinceID != "" {
		startAfter = eventKey(boardID, sinceID)
	}
	keys, err := s.obj.ListObjects(ctx, eventsPrefix(boardID), startAfter)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(keys))
	prefix := eventsPrefix(boardID)
	for _, k := range keys {
		id := strings.TrimPrefix(k, prefix)
		id = strings.TrimSuffix(id, ".json")
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Snapshot writes a full board snapshot and updates the latest pointer.
// Called by clients whose Load saw "too many" events since the last
// snapshot. Failure to update the pointer is non-fatal — the next client
// will produce its own snapshot.
func (s *Store) Snapshot(ctx context.Context, boardID string, b Board) error {
	upToID := ""
	cached, err := s.local.LoadCachedEvents(ctx, boardID, "")
	if err != nil {
		return err
	}
	for _, e := range cached {
		if e.ID > upToID {
			upToID = e.ID
		}
	}
	snapID := s.newID()
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	key := snapKey(boardID, snapID)
	if err := s.obj.PutObject(ctx, key, data, "application/json"); err != nil {
		return err
	}
	ptr := SnapshotPointer{
		Key:     key,
		UpToID:  upToID,
		SavedAt: s.now(),
		SavedBy: s.author,
	}
	ptrData, err := json.Marshal(ptr)
	if err != nil {
		return err
	}
	return s.obj.PutObject(ctx, snapLatestKey(boardID), ptrData, "application/json")
}
