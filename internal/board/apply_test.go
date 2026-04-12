package board

import (
	"cmp"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"
)

// mkEvent builds an event with a pre-encoded payload.
func mkEvent(t *testing.T, id, author string, ts time.Time, op Op, payload any) Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return Event{ID: id, Board: "b", Author: author, Ts: ts, Op: op, Payload: raw}
}

func apply(t *testing.T, b Board, e Event) Board {
	t.Helper()
	out, err := Apply(b, e)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return out
}

func baseBoard() Board {
	created := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	return Board{
		BoardMeta: BoardMeta{
			ID:        "b",
			Title:     "Test",
			Columns:   []Column{{ID: "col1", Title: "Todo"}, {ID: "col2", Title: "Done"}},
			CreatedAt: created,
			CreatedBy: "esh",
			UpdatedAt: created,
		},
	}
}

func TestApplyBoardCreate(t *testing.T) {
	t.Parallel()
	b := Board{BoardMeta: BoardMeta{ID: "b"}}
	ts := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	e := mkEvent(t, "01", "esh", ts, OpBoardCreate, BoardCreatePayload{
		Title:       "New Board",
		Description: "desc",
		Columns:     []Column{{ID: "c1", Title: "Todo"}},
	})
	got := apply(t, b, e)
	if got.Title != "New Board" || got.Description != "desc" {
		t.Errorf("meta: %+v", got.BoardMeta)
	}
	if len(got.Columns) != 1 || got.Columns[0].ID != "c1" {
		t.Errorf("columns: %+v", got.Columns)
	}
	if !got.CreatedAt.Equal(ts) || got.CreatedBy != "esh" {
		t.Errorf("created fields: %v / %q", got.CreatedAt, got.CreatedBy)
	}
}

func TestApplyCardAdd(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)
	e := mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "Write intro", Column: "col1", Position: 1.0},
	})
	got := apply(t, b, e)
	if len(got.Cards) != 1 {
		t.Fatalf("cards len = %d", len(got.Cards))
	}
	c := got.Cards[0]
	if c.ID != "k1" || c.Title != "Write intro" {
		t.Errorf("card: %+v", c)
	}
	if !c.CreatedAt.Equal(ts) || c.CreatedBy != "esh" {
		t.Errorf("created: %v / %q", c.CreatedAt, c.CreatedBy)
	}
}

func TestApplyCardAddDuplicateIsNoop(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	add := mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "First", Column: "col1", Position: 1.0},
	})
	b = apply(t, b, add)
	dup := mkEvent(t, "02", "alice", ts, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "Second", Column: "col2", Position: 2.0},
	})
	got := apply(t, b, dup)
	if len(got.Cards) != 1 || got.Cards[0].Title != "First" {
		t.Errorf("expected dup no-op, got %+v", got.Cards)
	}
}

func TestApplyCardPatchFieldIndependence(t *testing.T) {
	t.Parallel()
	// Alice patches title at t=1; Bob patches description at t=2.
	// Both must survive — the whole point of granular ops.
	b := baseBoard()
	ts0 := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts0, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Title: "orig title", Description: "orig desc", Column: "col1", Position: 1.0},
	}))

	newTitle := "new title"
	newDesc := "new desc"
	alice := mkEvent(t, "02", "alice", ts0.Add(time.Second), OpCardPatch, CardPatchPayload{
		ID:    "k1",
		Title: &newTitle,
	})
	bob := mkEvent(t, "03", "bob", ts0.Add(2*time.Second), OpCardPatch, CardPatchPayload{
		ID:          "k1",
		Description: &newDesc,
	})
	b = apply(t, b, alice)
	b = apply(t, b, bob)

	if b.Cards[0].Title != "new title" || b.Cards[0].Description != "new desc" {
		t.Errorf("expected both patches applied, got: %+v", b.Cards[0])
	}
	if b.Cards[0].UpdatedBy != "bob" {
		t.Errorf("updated_by = %q, want bob", b.Cards[0].UpdatedBy)
	}
}

func TestApplyCardPatchMissingIsNoop(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	title := "x"
	e := mkEvent(t, "01", "esh", time.Now(), OpCardPatch, CardPatchPayload{ID: "nonexistent", Title: &title})
	got := apply(t, b, e)
	if len(got.Cards) != 0 {
		t.Errorf("cards: %+v", got.Cards)
	}
}

func TestApplyCardMove(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{
		Card: Card{ID: "k1", Column: "col1", Position: 1.0},
	}))
	b = apply(t, b, mkEvent(t, "02", "esh", ts.Add(time.Second), OpCardMove, CardMovePayload{
		ID: "k1", Column: "col2", Position: 3.5,
	}))
	if b.Cards[0].Column != "col2" || b.Cards[0].Position != 3.5 {
		t.Errorf("card: %+v", b.Cards[0])
	}
}

func TestApplyCardDelete(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "col1", Position: 1.0}}))
	b = apply(t, b, mkEvent(t, "02", "esh", ts, OpCardAdd, CardAddPayload{Card: Card{ID: "k2", Column: "col1", Position: 2.0}}))
	b = apply(t, b, mkEvent(t, "03", "esh", ts, OpCardDelete, CardDeletePayload{ID: "k1"}))
	if len(b.Cards) != 1 || b.Cards[0].ID != "k2" {
		t.Errorf("cards: %+v", b.Cards)
	}
	// Subsequent ops on deleted card are no-ops, not errors.
	title := "x"
	b = apply(t, b, mkEvent(t, "04", "esh", ts, OpCardPatch, CardPatchPayload{ID: "k1", Title: &title}))
	if len(b.Cards) != 1 {
		t.Errorf("expected patch-after-delete to be no-op")
	}
}

func TestApplyColumnAddRenameDelete(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts, OpColumnAdd, ColumnAddPayload{Column: Column{ID: "col3", Title: "Blocked"}}))
	if len(b.Columns) != 3 {
		t.Fatalf("columns: %+v", b.Columns)
	}
	wip := 5
	b = apply(t, b, mkEvent(t, "02", "esh", ts, OpColumnRename, ColumnRenamePayload{ID: "col3", Title: "Blocked!", WIP: &wip}))
	if b.Columns[2].Title != "Blocked!" || b.Columns[2].WIP != 5 {
		t.Errorf("after rename: %+v", b.Columns[2])
	}
	b = apply(t, b, mkEvent(t, "03", "esh", ts, OpColumnDelete, ColumnDeletePayload{ID: "col3"}))
	if len(b.Columns) != 2 {
		t.Errorf("after delete: %+v", b.Columns)
	}
}

func TestApplyColumnReorder(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	b = apply(t, b, mkEvent(t, "01", "esh", time.Now(), OpColumnAdd, ColumnAddPayload{Column: Column{ID: "col3", Title: "Blocked"}}))
	// Reorder with a missing ID and leaving one out — both should be handled.
	e := mkEvent(t, "02", "esh", time.Now(), OpColumnReorder, ColumnReorderPayload{
		ColumnIDs: []string{"col3", "col1", "ghost"},
	})
	b = apply(t, b, e)
	gotIDs := []string{b.Columns[0].ID, b.Columns[1].ID, b.Columns[2].ID}
	wantIDs := []string{"col3", "col1", "col2"} // ghost dropped, col2 appended
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("reorder: got %v, want %v", gotIDs, wantIDs)
	}
}

func TestApplyCommentAppendOnly(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "col1", Position: 1.0}}))
	b = apply(t, b, mkEvent(t, "02", "alice", ts.Add(time.Second), OpCommentAdd, CommentAddPayload{
		CardID: "k1", Comment: Comment{ID: "cm1", Text: "first"},
	}))
	b = apply(t, b, mkEvent(t, "03", "bob", ts.Add(2*time.Second), OpCommentAdd, CommentAddPayload{
		CardID: "k1", Comment: Comment{ID: "cm2", Text: "second"},
	}))
	cms := b.Cards[0].Comments
	if len(cms) != 2 || cms[0].Text != "first" || cms[1].Text != "second" {
		t.Errorf("comments: %+v", cms)
	}
	// Author + ts defaults pulled from event when comment omits them.
	if cms[0].Author != "alice" || !cms[0].Ts.Equal(ts.Add(time.Second)) {
		t.Errorf("comment defaults: %+v", cms[0])
	}
}

func TestApplyChecklistToggleAndDelete(t *testing.T) {
	t.Parallel()
	b := baseBoard()
	ts := time.Now()
	b = apply(t, b, mkEvent(t, "01", "esh", ts, OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Column: "col1", Position: 1.0}}))
	b = apply(t, b, mkEvent(t, "02", "esh", ts, OpChecklistAdd, ChecklistAddPayload{
		CardID: "k1", Item: ChecklistItem{ID: "i1", Text: "outline"},
	}))
	b = apply(t, b, mkEvent(t, "03", "esh", ts, OpChecklistAdd, ChecklistAddPayload{
		CardID: "k1", Item: ChecklistItem{ID: "i2", Text: "draft"},
	}))
	b = apply(t, b, mkEvent(t, "04", "esh", ts, OpChecklistToggle, ChecklistTogglePayload{CardID: "k1", ItemID: "i1"}))
	if !b.Cards[0].Checklist[0].Done {
		t.Errorf("i1 not toggled on")
	}
	b = apply(t, b, mkEvent(t, "05", "esh", ts, OpChecklistToggle, ChecklistTogglePayload{CardID: "k1", ItemID: "i1"}))
	if b.Cards[0].Checklist[0].Done {
		t.Errorf("i1 not toggled off")
	}
	b = apply(t, b, mkEvent(t, "06", "esh", ts, OpChecklistDelete, ChecklistDeletePayload{CardID: "k1", ItemID: "i1"}))
	if len(b.Cards[0].Checklist) != 1 || b.Cards[0].Checklist[0].ID != "i2" {
		t.Errorf("after delete: %+v", b.Cards[0].Checklist)
	}
}

// TestApplyDeterminism: shuffle a fixture event list, sort by ULID, fold,
// confirm the result is always identical. This is the property that lets us
// trust concurrent clients will converge.
func TestApplyDeterminism(t *testing.T) {
	t.Parallel()
	events := buildFixtureEvents(t)

	// Canonical fold: sort by ID and fold in order.
	slices.SortFunc(events, func(a, b Event) int { return cmp.Compare(a.ID, b.ID) })
	want := foldAll(t, events)

	rng := rand.New(rand.NewPCG(1, 1))
	for trial := 0; trial < 20; trial++ {
		shuffled := append([]Event(nil), events...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		slices.SortFunc(shuffled, func(a, b Event) int { return cmp.Compare(a.ID, b.ID) })
		got := foldAll(t, shuffled)
		if !boardsEqual(got, want) {
			t.Fatalf("trial %d: non-deterministic fold", trial)
		}
	}
}

func buildFixtureEvents(t *testing.T) []Event {
	t.Helper()
	ts := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	title1 := "Written intro"
	desc1 := "Expanded description"
	return []Event{
		mkEvent(t, "01AAAA", "esh", ts, OpBoardCreate, BoardCreatePayload{
			Title:   "Test",
			Columns: []Column{{ID: "col1", Title: "Todo"}, {ID: "col2", Title: "Done"}},
		}),
		mkEvent(t, "01BBBB", "esh", ts.Add(1*time.Second), OpCardAdd, CardAddPayload{
			Card: Card{ID: "k1", Title: "Write intro", Column: "col1", Position: 1.0},
		}),
		mkEvent(t, "01CCCC", "esh", ts.Add(2*time.Second), OpCardAdd, CardAddPayload{
			Card: Card{ID: "k2", Title: "Review", Column: "col1", Position: 2.0},
		}),
		mkEvent(t, "01DDDD", "alice", ts.Add(3*time.Second), OpCardPatch, CardPatchPayload{
			ID: "k1", Title: &title1,
		}),
		mkEvent(t, "01EEEE", "bob", ts.Add(4*time.Second), OpCardPatch, CardPatchPayload{
			ID: "k1", Description: &desc1,
		}),
		mkEvent(t, "01FFFF", "esh", ts.Add(5*time.Second), OpCardMove, CardMovePayload{
			ID: "k1", Column: "col2", Position: 1.0,
		}),
		mkEvent(t, "01GGGG", "alice", ts.Add(6*time.Second), OpCommentAdd, CommentAddPayload{
			CardID: "k1", Comment: Comment{ID: "cm1", Text: "nice"},
		}),
	}
}

func foldAll(t *testing.T, events []Event) Board {
	t.Helper()
	b := Board{BoardMeta: BoardMeta{ID: "b"}}
	for _, e := range events {
		b = apply(t, b, e)
	}
	return b
}

// boardsEqual compares two boards for fold-equivalence. We can't use
// reflect.DeepEqual directly because nil vs empty slices differ.
func boardsEqual(a, b Board) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func TestApplyMalformedPayloadReturnsError(t *testing.T) {
	t.Parallel()
	e := Event{ID: "01", Op: OpCardAdd, Payload: []byte(`{"card": "not an object"}`)}
	_, err := Apply(Board{}, e)
	if err == nil {
		t.Error("expected error from malformed payload")
	}
}
