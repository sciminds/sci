package board

import (
	"context"
	"fmt"
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
