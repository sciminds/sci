package board

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBoardJSONRoundTrip(t *testing.T) {
	due := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 5, 15, 23, 59, 0, 0, time.UTC)
	created := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	orig := Board{
		BoardMeta: BoardMeta{
			ID:          "research-q2",
			Title:       "Research Q2",
			Description: "Lab kanban for Q2 deliverables",
			Columns: []Column{
				{ID: "col1", Title: "Todo"},
				{ID: "col2", Title: "Doing", WIP: 3},
				{ID: "col3", Title: "Done"},
			},
			CreatedAt: created,
			CreatedBy: "esh",
			UpdatedAt: created,
		},
		Cards: []Card{
			{
				ID:          "card1",
				Title:       "Write intro",
				Description: "# Intro\n\nDraft the first section.",
				Column:      "col2",
				Position:    1.5,
				Priority:    "high",
				Labels:      []string{"paper", "draft"},
				Assignees:   []string{"esh", "alice"},
				DueDate:     &due,
				Deadline:    &deadline,
				Checklist: []ChecklistItem{
					{ID: "chk1", Text: "Outline", Done: true},
					{ID: "chk2", Text: "Draft", Done: false},
				},
				Comments: []Comment{
					{ID: "cm1", Author: "alice", Text: "Looks good", Ts: created},
				},
				CreatedAt: created,
				UpdatedAt: created,
				CreatedBy: "esh",
				UpdatedBy: "esh",
			},
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Board
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Compare field-by-field where equality is well-defined.
	if got.ID != orig.ID || got.Title != orig.Title || got.Description != orig.Description {
		t.Errorf("meta fields changed: got %+v", got.BoardMeta)
	}
	if len(got.Columns) != 3 || got.Columns[1].WIP != 3 {
		t.Errorf("columns: %+v", got.Columns)
	}
	if len(got.Cards) != 1 {
		t.Fatalf("cards len = %d, want 1", len(got.Cards))
	}
	c := got.Cards[0]
	if c.ID != "card1" || c.Title != "Write intro" || c.Position != 1.5 {
		t.Errorf("card basics: %+v", c)
	}
	if c.DueDate == nil || !c.DueDate.Equal(due) {
		t.Errorf("due date: %v", c.DueDate)
	}
	if c.Deadline == nil || !c.Deadline.Equal(deadline) {
		t.Errorf("deadline: %v", c.Deadline)
	}
	if len(c.Checklist) != 2 || c.Checklist[0].Done != true {
		t.Errorf("checklist: %+v", c.Checklist)
	}
	if len(c.Comments) != 1 || c.Comments[0].Author != "alice" {
		t.Errorf("comments: %+v", c.Comments)
	}
}

func TestCardJSONNilDatesOmitted(t *testing.T) {
	c := Card{ID: "x", Title: "t", Column: "c1", Position: 1.0}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "due_date") || strings.Contains(s, "deadline") {
		t.Errorf("expected nil dates to be omitted, got: %s", s)
	}
}

func TestBoardJSONUnknownFieldsIgnored(t *testing.T) {
	// Forward compat: adding fields to the schema should not break old clients.
	raw := `{
		"id": "b1",
		"title": "Test",
		"columns": [],
		"cards": [],
		"created_at": "2026-04-01T10:00:00Z",
		"created_by": "esh",
		"updated_at": "2026-04-01T10:00:00Z",
		"future_field": {"nested": true},
		"another_unknown": 42
	}`
	var b Board
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("unknown fields should be ignored: %v", err)
	}
	if b.ID != "b1" {
		t.Errorf("id = %q", b.ID)
	}
}

func TestTimeFieldsUseRFC3339(t *testing.T) {
	created := time.Date(2026, 4, 1, 10, 30, 45, 0, time.UTC)
	b := Board{BoardMeta: BoardMeta{ID: "b", Title: "t", CreatedAt: created, CreatedBy: "esh", UpdatedAt: created}}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "2026-04-01T10:30:45Z") {
		t.Errorf("expected RFC3339 timestamp, got: %s", data)
	}
}
