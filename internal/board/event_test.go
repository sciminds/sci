package board

import (
	"errors"
	"sort"
	"testing"
	"time"
)

func TestEventPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	due := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	title := "New title"

	cases := []struct {
		name    string
		op      Op
		payload any
	}{
		{"board.create", OpBoardCreate, BoardCreatePayload{Title: "B", Columns: []Column{{ID: "c1", Title: "Todo"}}}},
		{"board.update", OpBoardUpdate, BoardUpdatePayload{Title: &title}},
		{"column.add", OpColumnAdd, ColumnAddPayload{Column: Column{ID: "c2", Title: "Doing"}}},
		{"column.rename", OpColumnRename, ColumnRenamePayload{ID: "c1", Title: "Backlog"}},
		{"column.reorder", OpColumnReorder, ColumnReorderPayload{ColumnIDs: []string{"c2", "c1"}}},
		{"column.delete", OpColumnDelete, ColumnDeletePayload{ID: "c1"}},
		{"card.add", OpCardAdd, CardAddPayload{Card: Card{ID: "k1", Title: "T", Column: "c1", Position: 1.0}}},
		{"card.patch", OpCardPatch, CardPatchPayload{ID: "k1", Title: &title, DueDate: &due}},
		{"card.move", OpCardMove, CardMovePayload{ID: "k1", Column: "c2", Position: 2.5}},
		{"card.delete", OpCardDelete, CardDeletePayload{ID: "k1"}},
		{"card.comment.add", OpCommentAdd, CommentAddPayload{CardID: "k1", Comment: Comment{ID: "cm1", Author: "esh", Text: "hi"}}},
		{"card.checklist.add", OpChecklistAdd, ChecklistAddPayload{CardID: "k1", Item: ChecklistItem{ID: "i1", Text: "todo"}}},
		{"card.checklist.toggle", OpChecklistToggle, ChecklistTogglePayload{CardID: "k1", ItemID: "i1"}},
		{"card.checklist.delete", OpChecklistDelete, ChecklistDeletePayload{CardID: "k1", ItemID: "i1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := EncodePayload(tc.payload)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			e := Event{ID: "01HX", Board: "b", Author: "esh", Ts: time.Now(), Op: tc.op, Payload: raw}
			got, err := DecodePayload(e)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got == nil {
				t.Fatal("got nil payload")
			}
			// Re-encode the decoded value and compare to the original raw bytes.
			reRaw, err := EncodePayload(got)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if string(reRaw) != string(raw) {
				t.Errorf("round-trip mismatch:\nwant %s\ngot  %s", raw, reRaw)
			}
		})
	}
}

func TestDecodeUnknownOp(t *testing.T) {
	t.Parallel()
	e := Event{ID: "01HX", Op: Op("mystery.op"), Payload: []byte("{}")}
	_, err := DecodePayload(e)
	if err == nil {
		t.Fatal("expected error")
	}
	var uoe *UnknownOpError
	if !errors.As(err, &uoe) {
		t.Errorf("want UnknownOpError, got %T: %v", err, err)
	}
	if uoe.Op != "mystery.op" {
		t.Errorf("uoe.Op = %q", uoe.Op)
	}
}

func TestEventULIDOrdering(t *testing.T) {
	t.Parallel()
	// ULIDs are lexicographically sortable — sorting by ID == sorting by time.
	// We don't need a real ULID library here; the test asserts the contract
	// that Apply will rely on: lex sort matches insertion order for our IDs.
	events := []Event{
		{ID: "01HX000000000000000000000C"},
		{ID: "01HX000000000000000000000A"},
		{ID: "01HX000000000000000000000B"},
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	want := []string{
		"01HX000000000000000000000A",
		"01HX000000000000000000000B",
		"01HX000000000000000000000C",
	}
	for i, e := range events {
		if e.ID != want[i] {
			t.Errorf("[%d] = %q, want %q", i, e.ID, want[i])
		}
	}
}
