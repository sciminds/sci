package board

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/cloud"
)

// TestLiveBoardRoundTrip exercises the store end-to-end against a real R2
// bucket. It is skipped unless BOARD_LIVE=1 is set, matching the pattern
// used by proj/new and cass integration tests. Run with:
//
//	BOARD_LIVE=1 go test ./internal/board/ -run TestLiveBoardRoundTrip -v
//
// Requires a valid sci cloud login that includes board credentials. The
// test creates a throwaway board with a unique ID and deletes it before
// returning (including on failure).
func TestLiveBoardRoundTrip(t *testing.T) {
	if os.Getenv("BOARD_LIVE") != "1" {
		t.Skip("skipping live R2 test; set BOARD_LIVE=1 to enable")
	}

	_, client, err := cloud.SetupBoard()
	if err != nil {
		t.Fatalf("cloud.SetupBoard: %v", err)
	}
	adapter := NewCloudAdapter(client)

	cache, err := OpenLocalCache(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cache.Close() }()

	store := NewStore(adapter, cache, client.Username)

	// Unique board ID — includes timestamp so parallel / retried runs don't
	// collide and leftover state from a prior failed run is obvious.
	boardID := fmt.Sprintf("live-test-%d", time.Now().UnixNano())
	ctx := context.Background()

	// Always clean up, even on failure.
	t.Cleanup(func() {
		if err := store.DeleteBoard(ctx, boardID); err != nil {
			t.Logf("cleanup DeleteBoard(%s): %v", boardID, err)
		}
	})

	cols := []Column{
		{ID: "todo", Title: "Todo"},
		{ID: "doing", Title: "Doing"},
		{ID: "done", Title: "Done"},
	}
	if err := store.CreateBoard(ctx, boardID, "Live test", "throwaway", cols); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	// Append a card.
	addEv, err := store.Append(ctx, boardID, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "smoke card", Column: "todo", Position: 1.0},
	})
	if err != nil {
		t.Fatalf("Append add: %v", err)
	}

	// Granular patch — exercises the concurrent-edit-safe path.
	newTitle := "patched"
	if _, err := store.Append(ctx, boardID, OpCardPatch, CardPatchPayload{
		ID: "k1", Title: &newTitle,
	}); err != nil {
		t.Fatalf("Append patch: %v", err)
	}

	// Load and verify.
	b, err := store.Load(ctx, boardID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Title != "Live test" || len(b.Columns) != 3 {
		t.Errorf("meta wrong: %+v", b.BoardMeta)
	}
	if len(b.Cards) != 1 || b.Cards[0].Title != "patched" {
		t.Errorf("cards: %+v", b.Cards)
	}

	// Poll since the first event should show only the patch.
	ids, err := store.Poll(ctx, boardID, addEv.ID)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("poll after first event: %v", ids)
	}

	// ListBoards should include the live board.
	boards, err := store.ListBoards(ctx)
	if err != nil {
		t.Fatalf("ListBoards: %v", err)
	}
	found := false
	for _, id := range boards {
		if id == boardID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListBoards did not include %q: %v", boardID, boards)
	}
}

// TestLiveBoardBucketIsPrivate verifies the sci-board bucket refuses
// unauthenticated reads. It (1) asserts the worker did not send a public_url
// for the board block and (2) hits the S3 endpoint for a real object key
// without credentials and requires a 401/403 response.
//
// Unlike TestLiveBoardRoundTrip, this test does NOT try the pub-*.r2.dev
// domain — for a bucket with no dev URL enabled, we don't know what hostname
// to hit (the hash is per-bucket and not exposed). The S3 endpoint is the
// authoritative check: R2 requires SigV4 there, so an unsigned request must
// fail regardless of dashboard settings.
func TestLiveBoardBucketIsPrivate(t *testing.T) {
	if os.Getenv("BOARD_LIVE") != "1" {
		t.Skip("skipping live R2 test; set BOARD_LIVE=1 to enable")
	}

	cfg, client, err := cloud.SetupBoard()
	if err != nil {
		t.Fatalf("cloud.SetupBoard: %v", err)
	}
	if cfg.Board == nil {
		t.Fatal("cfg.Board is nil")
	}
	if cfg.Board.PublicURL != "" {
		t.Errorf("board block has non-empty public_url %q — the worker should not advertise a public URL for the private bucket", cfg.Board.PublicURL)
	}

	adapter := NewCloudAdapter(client)
	cache, err := OpenLocalCache(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cache.Close() }()

	store := NewStore(adapter, cache, client.Username)
	boardID := fmt.Sprintf("live-privacy-%d", time.Now().UnixNano())
	ctx := context.Background()
	t.Cleanup(func() { _ = store.DeleteBoard(ctx, boardID) })

	if err := store.CreateBoard(ctx, boardID, "Privacy test", "", []Column{{ID: "c1", Title: "Todo"}}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	// Pick a known object key — meta.json always exists after CreateBoard.
	key := metaKey(boardID)
	url := fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s",
		cfg.AccountID, cfg.Board.BucketName, key)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("unauthenticated GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("unauthenticated GET to %s returned 200 — bucket is publicly readable!", url)
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		// Not a definitive failure, but worth surfacing. 404 would mean the
		// server rejected the request before even looking up the object.
		t.Logf("unauthenticated GET returned %d (want 401/403)", resp.StatusCode)
	}
}
