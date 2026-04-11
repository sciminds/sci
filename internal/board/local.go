package board

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// LocalCache is an on-disk SQLite cache for the board package. It stores
// every event we've downloaded (so offline launches still show the board),
// a queue of events we've applied locally but not yet uploaded, and the
// per-board sync state that tells [Store.Load] which events it still needs.
//
// The cache is intentionally separate from the sci-go pocketbase/dbx
// machinery — it follows the same raw database/sql + modernc/sqlite pattern
// as dbtui and markdb to keep dependencies minimal.
type LocalCache struct {
	db *sql.DB
}

// SyncState captures what a client knows about a board's sync status.
// LastSeenEventID is the highest event ULID the client has downloaded; the
// store passes it to LIST as start-after so we only fetch new events.
type SyncState struct {
	BoardID          string
	LastSeenEventID  string
	LastSnapshotKey  string
	LastSnapshotUpTo string
	UpdatedAt        time.Time
}

// OpenLocalCache opens (or creates) a SQLite cache at path. The schema is
// applied idempotently on every open.
func OpenLocalCache(path string) (*LocalCache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}
	c := &LocalCache{db: db}
	if err := c.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the cache DB.
func (c *LocalCache) Close() error {
	return c.db.Close()
}

func (c *LocalCache) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events_cached (
			board_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			author   TEXT NOT NULL,
			ts       TEXT NOT NULL,
			op       TEXT NOT NULL,
			payload  BLOB NOT NULL,
			PRIMARY KEY (board_id, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_cached_board_id ON events_cached(board_id, event_id)`,
		`CREATE TABLE IF NOT EXISTS events_pending (
			rowid      INTEGER PRIMARY KEY AUTOINCREMENT,
			board_id   TEXT NOT NULL,
			event_id   TEXT NOT NULL,
			author     TEXT NOT NULL,
			ts         TEXT NOT NULL,
			op         TEXT NOT NULL,
			payload    BLOB NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_pending_board ON events_pending(board_id, rowid)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			board_id             TEXT PRIMARY KEY,
			last_seen_event_id   TEXT NOT NULL DEFAULT '',
			last_snapshot_key    TEXT NOT NULL DEFAULT '',
			last_snapshot_up_to  TEXT NOT NULL DEFAULT '',
			updated_at           TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS boards_meta (
			board_id   TEXT PRIMARY KEY,
			meta       BLOB NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := c.db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// CacheEvents inserts events into events_cached. Conflicts on (board_id,
// event_id) are ignored, so re-downloading an event is safe.
func (c *LocalCache) CacheEvents(ctx context.Context, boardID string, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	const stmt = `INSERT OR IGNORE INTO events_cached(board_id, event_id, author, ts, op, payload) VALUES(?,?,?,?,?,?)`
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, stmt, boardID, e.ID, e.Author, e.Ts.Format(time.RFC3339Nano), string(e.Op), []byte(e.Payload)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadCachedEvents returns cached events for a board with event_id > sinceID.
// Pass an empty sinceID to load everything. Results are sorted by event_id.
func (c *LocalCache) LoadCachedEvents(ctx context.Context, boardID, sinceID string) ([]Event, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT event_id, author, ts, op, payload FROM events_cached
		 WHERE board_id = ? AND event_id > ? ORDER BY event_id`,
		boardID, sinceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEvents(rows, boardID)
}

// QueuePending appends a locally-applied event that still needs uploading.
func (c *LocalCache) QueuePending(ctx context.Context, boardID string, e Event) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO events_pending(board_id, event_id, author, ts, op, payload, created_at) VALUES(?,?,?,?,?,?,?)`,
		boardID, e.ID, e.Author, e.Ts.Format(time.RFC3339Nano), string(e.Op), []byte(e.Payload),
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// PendingEvents returns queued events for a board in insertion order.
func (c *LocalCache) PendingEvents(ctx context.Context, boardID string) ([]Event, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT event_id, author, ts, op, payload FROM events_pending
		 WHERE board_id = ? ORDER BY rowid`, boardID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEvents(rows, boardID)
}

// RemovePending removes a pending event after it has been successfully
// uploaded. Calling with an unknown event_id is not an error.
func (c *LocalCache) RemovePending(ctx context.Context, boardID, eventID string) error {
	_, err := c.db.ExecContext(ctx,
		`DELETE FROM events_pending WHERE board_id = ? AND event_id = ?`,
		boardID, eventID)
	return err
}

// SetSyncState upserts the sync state row for a board.
func (c *LocalCache) SetSyncState(ctx context.Context, s SyncState) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO sync_state(board_id, last_seen_event_id, last_snapshot_key, last_snapshot_up_to, updated_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(board_id) DO UPDATE SET
		   last_seen_event_id  = excluded.last_seen_event_id,
		   last_snapshot_key   = excluded.last_snapshot_key,
		   last_snapshot_up_to = excluded.last_snapshot_up_to,
		   updated_at          = excluded.updated_at`,
		s.BoardID, s.LastSeenEventID, s.LastSnapshotKey, s.LastSnapshotUpTo,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// GetSyncState returns the sync state for a board. A board with no row yet
// returns a zero-value SyncState with BoardID set — not an error.
func (c *LocalCache) GetSyncState(ctx context.Context, boardID string) (SyncState, error) {
	row := c.db.QueryRowContext(ctx,
		`SELECT last_seen_event_id, last_snapshot_key, last_snapshot_up_to, updated_at
		 FROM sync_state WHERE board_id = ?`, boardID)
	s := SyncState{BoardID: boardID}
	var updatedAt string
	if err := row.Scan(&s.LastSeenEventID, &s.LastSnapshotKey, &s.LastSnapshotUpTo, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s, nil
		}
		return s, err
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		s.UpdatedAt = t
	}
	return s, nil
}

// CacheBoardMeta upserts a board's metadata.
func (c *LocalCache) CacheBoardMeta(ctx context.Context, meta BoardMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx,
		`INSERT INTO boards_meta(board_id, meta, updated_at) VALUES(?,?,?)
		 ON CONFLICT(board_id) DO UPDATE SET meta = excluded.meta, updated_at = excluded.updated_at`,
		meta.ID, data, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// LoadBoardMeta returns cached metadata for a board, or (zero, sql.ErrNoRows)
// if no row exists.
func (c *LocalCache) LoadBoardMeta(ctx context.Context, boardID string) (BoardMeta, error) {
	row := c.db.QueryRowContext(ctx, `SELECT meta FROM boards_meta WHERE board_id = ?`, boardID)
	var data []byte
	if err := row.Scan(&data); err != nil {
		return BoardMeta{}, err
	}
	var meta BoardMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return BoardMeta{}, err
	}
	return meta, nil
}

// ListCachedBoards returns the IDs of all boards with a cached meta row.
func (c *LocalCache) ListCachedBoards(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT board_id FROM boards_meta ORDER BY board_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanEvents(rows *sql.Rows, boardID string) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var e Event
		var tsStr string
		var op string
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Author, &tsStr, &op, &payload); err != nil {
			return nil, err
		}
		e.Board = boardID
		e.Op = Op(op)
		e.Payload = payload
		if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			e.Ts = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
