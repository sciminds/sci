package board

import (
	"encoding/json"
	"fmt"
	"time"
)

// Op is the discrete mutation type carried by an [Event]. Ops are granular
// so that concurrent edits to different fields of the same card both survive
// the fold — see apply.go for resolution semantics.
type Op string

// Event operation types.
const (
	OpBoardCreate Op = "board.create"
	OpBoardUpdate Op = "board.update"
	OpBoardDelete Op = "board.delete"

	OpColumnAdd     Op = "column.add"
	OpColumnRename  Op = "column.rename"
	OpColumnReorder Op = "column.reorder"
	OpColumnDelete  Op = "column.delete"

	OpCardAdd    Op = "card.add"
	OpCardPatch  Op = "card.patch"
	OpCardMove   Op = "card.move"
	OpCardDelete Op = "card.delete"

	OpCommentAdd Op = "card.comment.add"

	OpChecklistAdd    Op = "card.checklist.add"
	OpChecklistToggle Op = "card.checklist.toggle"
	OpChecklistDelete Op = "card.checklist.delete"
)

// Event is the unit of change written to R2. Each client only writes events
// under its own author prefix, so two clients can never produce colliding
// keys. The ID is a ULID — time-sortable, so sorting events by ID is the
// same as sorting by wall clock (modulo clock skew).
type Event struct {
	ID      string          `json:"id"`
	Board   string          `json:"board"`
	Author  string          `json:"author"`
	Ts      time.Time       `json:"ts"`
	Op      Op              `json:"op"`
	Payload json.RawMessage `json:"payload"`
}

// Payload types. One per Op. Nil pointer fields in patch payloads mean
// "no change" — clearing a field is not supported in v1.

// BoardCreatePayload is the payload for OpBoardCreate.
type BoardCreatePayload struct { //nolint:revive // name is established in the API
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Columns     []Column `json:"columns"`
}

// BoardUpdatePayload is the payload for OpBoardUpdate.
type BoardUpdatePayload struct { //nolint:revive // name is established in the API
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

// BoardDeletePayload is the payload for OpBoardDelete.
type BoardDeletePayload struct{} //nolint:revive // name is established in the API

// ColumnAddPayload is the payload for OpColumnAdd.
type ColumnAddPayload struct {
	Column Column `json:"column"`
}

// ColumnRenamePayload is the payload for OpColumnRename.
type ColumnRenamePayload struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	WIP   *int   `json:"wip,omitempty"`
}

// ColumnReorderPayload carries the full ordering of column IDs. Full
// replacement (rather than a swap op) keeps Apply simple and idempotent.
type ColumnReorderPayload struct {
	ColumnIDs []string `json:"column_ids"`
}

// ColumnDeletePayload is the payload for OpColumnDelete.
type ColumnDeletePayload struct {
	ID string `json:"id"`
}

// CardAddPayload is the payload for OpCardAdd.
type CardAddPayload struct {
	Card Card `json:"card"`
}

// CardPatchPayload is a partial update. Only non-nil fields are applied.
// Two concurrent patches to disjoint fields both survive because Apply
// merges per-field, not per-card.
type CardPatchPayload struct {
	ID          string     `json:"id"`
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Priority    *string    `json:"priority,omitempty"`
	Labels      *[]string  `json:"labels,omitempty"`
	Assignees   *[]string  `json:"assignees,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Deadline    *time.Time `json:"deadline,omitempty"`
}

// CardMovePayload is the payload for OpCardMove.
type CardMovePayload struct {
	ID       string  `json:"id"`
	Column   string  `json:"column"`
	Position float64 `json:"position"`
}

// CardDeletePayload is the payload for OpCardDelete.
type CardDeletePayload struct {
	ID string `json:"id"`
}

// CommentAddPayload is the payload for OpCommentAdd.
type CommentAddPayload struct {
	CardID  string  `json:"card_id"`
	Comment Comment `json:"comment"`
}

// ChecklistAddPayload is the payload for OpChecklistAdd.
type ChecklistAddPayload struct {
	CardID string        `json:"card_id"`
	Item   ChecklistItem `json:"item"`
}

// ChecklistTogglePayload is the payload for OpChecklistToggle.
type ChecklistTogglePayload struct {
	CardID string `json:"card_id"`
	ItemID string `json:"item_id"`
}

// ChecklistDeletePayload is the payload for OpChecklistDelete.
type ChecklistDeletePayload struct {
	CardID string `json:"card_id"`
	ItemID string `json:"item_id"`
}

// EncodePayload serializes a typed payload into an Event's Payload field.
func EncodePayload(p any) (json.RawMessage, error) {
	return json.Marshal(p)
}

// DecodePayload parses e.Payload into the typed struct for e.Op. Callers
// type-assert the returned any to the expected concrete type. Unknown ops
// return a [UnknownOpError].
func DecodePayload(e Event) (any, error) {
	switch e.Op {
	case OpBoardCreate:
		var p BoardCreatePayload
		return decodeInto(e.Payload, &p)
	case OpBoardUpdate:
		var p BoardUpdatePayload
		return decodeInto(e.Payload, &p)
	case OpBoardDelete:
		var p BoardDeletePayload
		return decodeInto(e.Payload, &p)
	case OpColumnAdd:
		var p ColumnAddPayload
		return decodeInto(e.Payload, &p)
	case OpColumnRename:
		var p ColumnRenamePayload
		return decodeInto(e.Payload, &p)
	case OpColumnReorder:
		var p ColumnReorderPayload
		return decodeInto(e.Payload, &p)
	case OpColumnDelete:
		var p ColumnDeletePayload
		return decodeInto(e.Payload, &p)
	case OpCardAdd:
		var p CardAddPayload
		return decodeInto(e.Payload, &p)
	case OpCardPatch:
		var p CardPatchPayload
		return decodeInto(e.Payload, &p)
	case OpCardMove:
		var p CardMovePayload
		return decodeInto(e.Payload, &p)
	case OpCardDelete:
		var p CardDeletePayload
		return decodeInto(e.Payload, &p)
	case OpCommentAdd:
		var p CommentAddPayload
		return decodeInto(e.Payload, &p)
	case OpChecklistAdd:
		var p ChecklistAddPayload
		return decodeInto(e.Payload, &p)
	case OpChecklistToggle:
		var p ChecklistTogglePayload
		return decodeInto(e.Payload, &p)
	case OpChecklistDelete:
		var p ChecklistDeletePayload
		return decodeInto(e.Payload, &p)
	default:
		return nil, &UnknownOpError{Op: e.Op}
	}
}

func decodeInto[T any](raw json.RawMessage, out *T) (any, error) {
	if len(raw) == 0 {
		return *out, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	return *out, nil
}

// UnknownOpError is returned by DecodePayload when e.Op is not a known Op.
// Callers may choose to skip the event (forward compat) or treat it as fatal.
type UnknownOpError struct {
	Op Op
}

func (e *UnknownOpError) Error() string {
	return fmt.Sprintf("unknown op: %q", e.Op)
}
