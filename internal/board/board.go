package board

import "time"

// BoardMeta is the non-card metadata of a board. It lives in meta.json and
// changes rarely (create, rename, column add/remove). Separated from [Board]
// so the on-disk meta file stays small regardless of card count.
type BoardMeta struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Columns     []Column  `json:"columns"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Board is a fully-folded snapshot: metadata plus the current set of cards.
// Apply returns a new Board on every event — it is never mutated in place.
type Board struct {
	BoardMeta
	Cards []Card `json:"cards"`
}

// Column is a vertical lane. WIP is an optional work-in-progress limit;
// zero means no limit.
type Column struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	WIP   int    `json:"wip,omitempty"`
}

// Card is a single work item. Position is a fractional index for ordering
// within the column — see [Between] in position.go for the insertion rule.
//
// DueDate and Deadline are distinct: DueDate is a soft target, Deadline is a
// hard cutoff. Either may be nil.
type Card struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Column      string          `json:"column"`
	Position    float64         `json:"position"`
	Priority    string          `json:"priority,omitempty"`
	Labels      []string        `json:"labels,omitempty"`
	Assignees   []string        `json:"assignees,omitempty"`
	DueDate     *time.Time      `json:"due_date,omitempty"`
	Deadline    *time.Time      `json:"deadline,omitempty"`
	Checklist   []ChecklistItem `json:"checklist,omitempty"`
	Comments    []Comment       `json:"comments,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CreatedBy   string          `json:"created_by"`
	UpdatedBy   string          `json:"updated_by,omitempty"`
}

// ChecklistItem is a single line in a card's checklist.
type ChecklistItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Comment is an append-only note on a card. Comments cannot be edited or
// deleted — the log is the conversation.
type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Text   string    `json:"text"`
	Ts     time.Time `json:"ts"`
}
