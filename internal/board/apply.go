package board

import (
	"fmt"
	"slices"

	"github.com/samber/lo"
)

// Apply folds a single event into a board and returns the new state. It is
// a pure function: the input board is not mutated, and the same (board,
// event) pair always produces the same result. Determinism is essential —
// every client folds the same event log and must reach the same state.
//
// Error semantics:
//
//   - A malformed payload returns an error; callers should log and skip.
//   - A well-formed event targeting a missing card/column is a no-op, not
//     an error. The event log is immutable history, so an op may legitimately
//     reference something a later op removed.
//   - board.delete is intentionally a no-op here; whole-board removal is
//     handled by the store layer (deleting the R2 prefix).
func Apply(b Board, e Event) (Board, error) {
	payload, err := DecodePayload(e)
	if err != nil {
		return b, fmt.Errorf("event %s (%s): %w", e.ID, e.Op, err)
	}

	switch p := payload.(type) {
	case BoardCreatePayload:
		return applyBoardCreate(b, e, p), nil
	case BoardUpdatePayload:
		return applyBoardUpdate(b, e, p), nil
	case BoardDeletePayload:
		return b, nil
	case ColumnAddPayload:
		return applyColumnAdd(b, e, p), nil
	case ColumnRenamePayload:
		return applyColumnRename(b, e, p), nil
	case ColumnReorderPayload:
		return applyColumnReorder(b, e, p), nil
	case ColumnDeletePayload:
		return applyColumnDelete(b, e, p), nil
	case CardAddPayload:
		return applyCardAdd(b, e, p), nil
	case CardPatchPayload:
		return applyCardPatch(b, e, p), nil
	case CardMovePayload:
		return applyCardMove(b, e, p), nil
	case CardDeletePayload:
		return applyCardDelete(b, e, p), nil
	case CommentAddPayload:
		return applyCommentAdd(b, e, p), nil
	case ChecklistAddPayload:
		return applyChecklistAdd(b, e, p), nil
	case ChecklistTogglePayload:
		return applyChecklistToggle(b, e, p), nil
	case ChecklistDeletePayload:
		return applyChecklistDelete(b, e, p), nil
	default:
		return b, fmt.Errorf("event %s: unhandled payload type %T", e.ID, p)
	}
}

// touch updates board-level UpdatedAt to track "most recent activity".
func touch(b Board, e Event) Board {
	b.UpdatedAt = e.Ts
	return b
}

func applyBoardCreate(b Board, e Event, p BoardCreatePayload) Board {
	b.Title = p.Title
	b.Description = p.Description
	b.Columns = slices.Clone(p.Columns)
	if b.CreatedAt.IsZero() {
		b.CreatedAt = e.Ts
		b.CreatedBy = e.Author
	}
	return touch(b, e)
}

func applyBoardUpdate(b Board, e Event, p BoardUpdatePayload) Board {
	if p.Title != nil {
		b.Title = *p.Title
	}
	if p.Description != nil {
		b.Description = *p.Description
	}
	return touch(b, e)
}

func applyColumnAdd(b Board, e Event, p ColumnAddPayload) Board {
	if _, ok := findColumn(b.Columns, p.Column.ID); ok {
		return b
	}
	cols := slices.Clone(b.Columns)
	cols = append(cols, p.Column)
	b.Columns = cols
	return touch(b, e)
}

func applyColumnRename(b Board, e Event, p ColumnRenamePayload) Board {
	idx, ok := findColumn(b.Columns, p.ID)
	if !ok {
		return b
	}
	cols := slices.Clone(b.Columns)
	cols[idx].Title = p.Title
	if p.WIP != nil {
		cols[idx].WIP = *p.WIP
	}
	b.Columns = cols
	return touch(b, e)
}

// applyColumnReorder reorders columns according to the payload's ID list.
// IDs in the payload that no longer exist are skipped; columns present in b
// but missing from the payload are appended in their original order. This
// makes the op robust against concurrent column.delete events.
func applyColumnReorder(b Board, e Event, p ColumnReorderPayload) Board {
	byID := lo.KeyBy(b.Columns, func(c Column) string {
		return c.ID
	})
	out := make([]Column, 0, len(b.Columns))
	seen := make(map[string]bool, len(p.ColumnIDs))
	for _, id := range p.ColumnIDs {
		if c, ok := byID[id]; ok && !seen[id] {
			out = append(out, c)
			seen[id] = true
		}
	}
	out = append(out, lo.Reject(b.Columns, func(c Column, _ int) bool {
		return seen[c.ID]
	})...)
	b.Columns = out
	return touch(b, e)
}

func applyColumnDelete(b Board, e Event, p ColumnDeletePayload) Board {
	idx, ok := findColumn(b.Columns, p.ID)
	if !ok {
		return b
	}
	cols := make([]Column, 0, len(b.Columns)-1)
	cols = append(cols, b.Columns[:idx]...)
	cols = append(cols, b.Columns[idx+1:]...)
	b.Columns = cols
	// Cards in the deleted column are left with their Column field intact.
	// The UI treats orphaned cards as hidden; a later card.move can rescue
	// them. Deleting cards here would be a lossy side-effect.
	return touch(b, e)
}

func applyCardAdd(b Board, e Event, p CardAddPayload) Board {
	if _, ok := findCard(b.Cards, p.Card.ID); ok {
		return b
	}
	card := p.Card
	if card.CreatedAt.IsZero() {
		card.CreatedAt = e.Ts
		card.CreatedBy = e.Author
	}
	card.UpdatedAt = e.Ts
	card.UpdatedBy = e.Author
	cards := slices.Clone(b.Cards)
	cards = append(cards, card)
	b.Cards = cards
	return touch(b, e)
}

func applyCardPatch(b Board, e Event, p CardPatchPayload) Board {
	idx, ok := findCard(b.Cards, p.ID)
	if !ok {
		return b
	}
	cards := slices.Clone(b.Cards)
	c := &cards[idx]
	if p.Title != nil {
		c.Title = *p.Title
	}
	if p.Description != nil {
		c.Description = *p.Description
	}
	if p.Priority != nil {
		c.Priority = *p.Priority
	}
	if p.Labels != nil {
		c.Labels = slices.Clone(*p.Labels)
	}
	if p.Assignees != nil {
		c.Assignees = slices.Clone(*p.Assignees)
	}
	if p.DueDate != nil {
		t := *p.DueDate
		c.DueDate = &t
	}
	if p.Deadline != nil {
		t := *p.Deadline
		c.Deadline = &t
	}
	c.UpdatedAt = e.Ts
	c.UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func applyCardMove(b Board, e Event, p CardMovePayload) Board {
	idx, ok := findCard(b.Cards, p.ID)
	if !ok {
		return b
	}
	cards := slices.Clone(b.Cards)
	cards[idx].Column = p.Column
	cards[idx].Position = p.Position
	cards[idx].UpdatedAt = e.Ts
	cards[idx].UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func applyCardDelete(b Board, e Event, p CardDeletePayload) Board {
	idx, ok := findCard(b.Cards, p.ID)
	if !ok {
		return b
	}
	cards := make([]Card, 0, len(b.Cards)-1)
	cards = append(cards, b.Cards[:idx]...)
	cards = append(cards, b.Cards[idx+1:]...)
	b.Cards = cards
	return touch(b, e)
}

func applyCommentAdd(b Board, e Event, p CommentAddPayload) Board {
	idx, ok := findCard(b.Cards, p.CardID)
	if !ok {
		return b
	}
	cards := slices.Clone(b.Cards)
	comments := slices.Clone(cards[idx].Comments)
	comment := p.Comment
	if comment.Ts.IsZero() {
		comment.Ts = e.Ts
	}
	if comment.Author == "" {
		comment.Author = e.Author
	}
	comments = append(comments, comment)
	cards[idx].Comments = comments
	cards[idx].UpdatedAt = e.Ts
	cards[idx].UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func applyChecklistAdd(b Board, e Event, p ChecklistAddPayload) Board {
	idx, ok := findCard(b.Cards, p.CardID)
	if !ok {
		return b
	}
	cards := slices.Clone(b.Cards)
	items := slices.Clone(cards[idx].Checklist)
	items = append(items, p.Item)
	cards[idx].Checklist = items
	cards[idx].UpdatedAt = e.Ts
	cards[idx].UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func applyChecklistToggle(b Board, e Event, p ChecklistTogglePayload) Board {
	cardIdx, ok := findCard(b.Cards, p.CardID)
	if !ok {
		return b
	}
	itemIdx := -1
	for i, it := range b.Cards[cardIdx].Checklist {
		if it.ID == p.ItemID {
			itemIdx = i
			break
		}
	}
	if itemIdx < 0 {
		return b
	}
	cards := slices.Clone(b.Cards)
	items := slices.Clone(cards[cardIdx].Checklist)
	items[itemIdx].Done = !items[itemIdx].Done
	cards[cardIdx].Checklist = items
	cards[cardIdx].UpdatedAt = e.Ts
	cards[cardIdx].UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func applyChecklistDelete(b Board, e Event, p ChecklistDeletePayload) Board {
	cardIdx, ok := findCard(b.Cards, p.CardID)
	if !ok {
		return b
	}
	itemIdx := -1
	for i, it := range b.Cards[cardIdx].Checklist {
		if it.ID == p.ItemID {
			itemIdx = i
			break
		}
	}
	if itemIdx < 0 {
		return b
	}
	cards := slices.Clone(b.Cards)
	old := cards[cardIdx].Checklist
	items := make([]ChecklistItem, 0, len(old)-1)
	items = append(items, old[:itemIdx]...)
	items = append(items, old[itemIdx+1:]...)
	cards[cardIdx].Checklist = items
	cards[cardIdx].UpdatedAt = e.Ts
	cards[cardIdx].UpdatedBy = e.Author
	b.Cards = cards
	return touch(b, e)
}

func findCard(cards []Card, id string) (int, bool) {
	for i, c := range cards {
		if c.ID == id {
			return i, true
		}
	}
	return -1, false
}

func findColumn(cols []Column, id string) (int, bool) {
	for i, c := range cols {
		if c.ID == id {
			return i, true
		}
	}
	return -1, false
}
