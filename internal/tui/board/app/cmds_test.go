package app

import (
	"context"
	"path/filepath"
	"testing"

	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/kit"
)

// ── listBoardsCmd ─────────────────────────────────────────────────────

func TestListBoardsCmdReturnsKitResult(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	cmd := listBoardsCmd(store)
	msg := cmd()
	r, ok := msg.(kit.Result[[]string])
	if !ok {
		t.Fatalf("expected kit.Result[[]string], got %T", msg)
	}
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if len(r.Value) == 0 {
		t.Error("expected at least one board")
	}
}

// ── loadBoardCmd ──────────────────────────────────────────────────────

func TestLoadBoardCmdReturnsKitResult(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	cmd := loadBoardCmd(store, "alpha")
	msg := cmd()
	r, ok := msg.(kit.Result[engine.Board])
	if !ok {
		t.Fatalf("expected kit.Result[engine.Board], got %T", msg)
	}
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Value.Title != "Alpha Board" {
		t.Errorf("title = %q, want %q", r.Value.Title, "Alpha Board")
	}
}

func TestLoadBoardCmdErrorReturnsKitResult(t *testing.T) {
	t.Parallel()
	// Build a store with no boards for a guaranteed error.
	obj := newFakeObjectStore()
	cachePath := filepath.Join(t.TempDir(), "board.db")
	local, err := engine.OpenLocalCache(cachePath)
	if err != nil {
		t.Fatalf("open local cache: %v", err)
	}
	t.Cleanup(func() { _ = local.Close() })
	store := engine.NewStore(obj, local, "tester")

	cmd := loadBoardCmd(store, "nonexistent")
	msg := cmd()
	r, ok := msg.(kit.Result[engine.Board])
	if !ok {
		t.Fatalf("expected kit.Result[engine.Board], got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error for nonexistent board")
	}
}

// ── AppendCmd ─────────────────────────────────────────────────────────

func TestAppendCmdReturnsKitResult(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	card := engine.Card{ID: "c99", Title: "New", Column: "todo", Position: 99}
	cmd := AppendCmd(store, "alpha", engine.OpCardAdd, engine.CardAddPayload{Card: card})
	msg := cmd()
	r, ok := msg.(kit.Result[struct{}])
	if !ok {
		t.Fatalf("expected kit.Result[struct{}], got %T", msg)
	}
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}

	// Verify the card was actually appended.
	b, err := store.Load(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	found := false
	for _, c := range b.Cards {
		if c.ID == "c99" {
			found = true
			break
		}
	}
	if !found {
		t.Error("card c99 not found after AppendCmd")
	}
}
