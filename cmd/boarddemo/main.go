// Command boarddemo launches the board TUI against an in-memory fake
// ObjectStore seeded with a rich fixture board. Use this to iterate on
// styles, spacing, and key handling without touching R2 auth.
//
//	go run ./cmd/boarddemo
//
// Not a shipping binary — there's no cmd/sci integration. Delete once
// cmd/board.go exists.
package main

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	engine "github.com/sciminds/cli/internal/board"
	btui "github.com/sciminds/cli/internal/tui/board"
)

func main() {
	store, err := seededStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}

	// Open the calendar on the current month so the demo feels live.
	currentMonth := int(time.Now().Month()) - 1 // 0-indexed
	if err := btui.Run(store, "calendar", btui.WithInitialGridCursor(currentMonth)); err != nil {
		if err == btui.ErrInterrupted {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "run:", err)
		os.Exit(1)
	}
}

func seededStore() (*engine.Store, error) {
	obj := newMemObjectStore()

	// Temp local cache — gets removed on exit. Demo is stateless.
	cacheDir, err := os.MkdirTemp("", "boarddemo-")
	if err != nil {
		return nil, err
	}
	local, err := engine.OpenLocalCache(filepath.Join(cacheDir, "cache.db"))
	if err != nil {
		return nil, err
	}

	store := engine.NewStore(obj, local, "demo")
	ctx := context.Background()

	if err := seedCalendar(ctx, store); err != nil {
		return nil, err
	}
	if err := seedCurrentQuarter(ctx, store); err != nil {
		return nil, err
	}
	return store, nil
}

// seedCalendar creates a 12-column board, one column per month, seeded
// with a spread of realistic research-workflow cards across the year.
// Exercises the horizontal-scroll and column-collapse code paths.
func seedCalendar(ctx context.Context, store *engine.Store) error {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	cols := lo.Map(months, func(mo string, _ int) engine.Column {
		return engine.Column{ID: strings.ToLower(mo), Title: mo}
	})
	if err := store.CreateBoard(ctx, "calendar", "Calendar", "year-at-a-glance", cols); err != nil {
		return err
	}

	now := time.Now()
	due := func(month time.Month, day int) *time.Time {
		t := time.Date(now.Year(), month, day, 17, 0, 0, 0, time.Local)
		return &t
	}

	seedCards(ctx, store, "calendar", []engine.Card{
		// Q1
		{ID: "cal-jan-1", Title: "NeurIPS author response", Column: "jan", Position: 1, Priority: "high", Labels: []string{"paper"}},
		{ID: "cal-jan-2", Title: "Renew IRB protocol", Column: "jan", Position: 2, Labels: []string{"admin"}},
		{ID: "cal-feb-1", Title: "Draft R01 specific aims", Column: "feb", Position: 1, Priority: "high", Labels: []string{"grant"}, DueDate: due(time.February, 15)},
		{ID: "cal-feb-2", Title: "Lab retreat planning", Column: "feb", Position: 2},
		{ID: "cal-mar-1", Title: "SfN abstract submission", Column: "mar", Position: 1, Priority: "med", Labels: []string{"conf"}, DueDate: due(time.March, 1)},
		{ID: "cal-mar-2", Title: "Pilot data analysis", Column: "mar", Position: 2, Labels: []string{"analysis"}},

		// Q2
		{ID: "cal-apr-1", Title: "CogSci submission", Column: "apr", Position: 1, Priority: "high", Labels: []string{"paper"}, DueDate: due(time.April, 8)},
		{ID: "cal-apr-2", Title: "Teach intro stats guest lecture", Column: "apr", Position: 2, Labels: []string{"teaching"}},
		{ID: "cal-may-1", Title: "Dissertation defense (S. Kim)", Column: "may", Position: 1, Priority: "high", Labels: []string{"mentoring"}},
		{ID: "cal-may-2", Title: "Review 2 NSF panels", Column: "may", Position: 2, Labels: []string{"service"}},
		{ID: "cal-jun-1", Title: "Rebuttal rebuttal rebuttal", Column: "jun", Position: 1, Labels: []string{"paper"}},

		// Q3
		{ID: "cal-jul-1", Title: "Summer school (Marseille)", Column: "jul", Position: 1, Priority: "med", Labels: []string{"travel"}},
		{ID: "cal-jul-2", Title: "Preregistration draft", Column: "jul", Position: 2, Labels: []string{"oss"}},
		{ID: "cal-aug-1", Title: "Field trip for collab data", Column: "aug", Position: 1, Labels: []string{"travel", "collab"}},
		{ID: "cal-sep-1", Title: "Start new rotation student", Column: "sep", Position: 1, Priority: "med", Labels: []string{"mentoring"}},
		{ID: "cal-sep-2", Title: "Annual progress report", Column: "sep", Position: 2, Labels: []string{"admin"}, DueDate: due(time.September, 30)},

		// Q4
		{ID: "cal-oct-1", Title: "SfN posters printed", Column: "oct", Position: 1, Priority: "high", Labels: []string{"conf"}},
		{ID: "cal-oct-2", Title: "NeurIPS camera ready", Column: "oct", Position: 2, Priority: "high", Labels: []string{"paper"}},
		{ID: "cal-nov-1", Title: "SfN talk", Column: "nov", Position: 1, Priority: "high", Labels: []string{"conf", "travel"}},
		{ID: "cal-nov-2", Title: "End-of-semester grading", Column: "nov", Position: 2, Labels: []string{"teaching"}},
		{ID: "cal-dec-1", Title: "Lab holiday party", Column: "dec", Position: 1, Labels: []string{"team"}},
		{ID: "cal-dec-2", Title: "Year-end writeup", Column: "dec", Position: 2, Labels: []string{"admin"}},
	})
	return nil
}

// seedCurrentQuarter is the traditional four-column kanban: Backlog →
// Todo → In Progress → Done. Exercises the stretch (all-fits) layout
// path and the rich card/detail rendering.
func seedCurrentQuarter(ctx context.Context, store *engine.Store) error {
	if err := store.CreateBoard(ctx, "current-quarter", "Current Quarter", "this quarter's work",
		[]engine.Column{
			{ID: "backlog", Title: "Backlog"},
			{ID: "todo", Title: "Todo"},
			{ID: "doing", Title: "In Progress", WIP: 3},
			{ID: "done", Title: "Done"},
		}); err != nil {
		return err
	}

	due := time.Now().Add(72 * time.Hour)
	seedCards(ctx, store, "current-quarter", []engine.Card{
		// Backlog
		{ID: "q-b1", Title: "Explore active-inference priors", Column: "backlog", Position: 1, Priority: "low", Labels: []string{"research"}},
		{ID: "q-b2", Title: "Read Tenenbaum 2011 follow-ups", Column: "backlog", Position: 2, Labels: []string{"reading"}},
		{ID: "q-b3", Title: "Sketch dataset release plan", Column: "backlog", Position: 3, Labels: []string{"oss"}},

		// Todo
		{ID: "q-t1", Title: "Write methods section", Column: "todo", Position: 1, Priority: "high", Labels: []string{"paper"}, DueDate: &due},
		{ID: "q-t2", Title: "Refactor preprocessing pipeline", Column: "todo", Position: 2, Priority: "med", Labels: []string{"code"}},
		{ID: "q-t3", Title: "Prep midterm review slides", Column: "todo", Position: 3, Labels: []string{"teaching"}},

		// In Progress
		{ID: "q-d1", Title: "Run sensitivity analysis", Column: "doing", Position: 1, Priority: "high", Labels: []string{"analysis"},
			Description: "sweep over the four key hyperparameters, record effect on held-out NLL.",
			Checklist: []engine.ChecklistItem{
				{ID: "s1", Text: "Define grid", Done: true},
				{ID: "s2", Text: "Run sweep", Done: true},
				{ID: "s3", Text: "Write up results", Done: false},
				{ID: "s4", Text: "Add to appendix", Done: false},
			},
			Comments: []engine.Comment{
				{ID: "cm1", Author: "demo", Text: "results are cleaner with log-spaced grid", Ts: time.Now().Add(-26 * time.Hour)},
				{ID: "cm2", Author: "demo", Text: "pushed raw logs to shared drive", Ts: time.Now().Add(-1 * time.Hour)},
			},
		},
		{ID: "q-d2", Title: "Draft cover letter", Column: "doing", Position: 2, Priority: "med", Labels: []string{"paper"}},

		// Done
		{ID: "q-done1", Title: "Set up repro environment", Column: "done", Position: 1, Labels: []string{"code"}},
		{ID: "q-done2", Title: "Collect baseline measurements", Column: "done", Position: 2, Labels: []string{"analysis"}},
		{ID: "q-done3", Title: "Register OSF project", Column: "done", Position: 3, Labels: []string{"oss"}},
	})
	return nil
}

func seedCards(ctx context.Context, store *engine.Store, boardID string, cards []engine.Card) {
	for _, c := range cards {
		if c.CreatedAt.IsZero() {
			c.CreatedAt = time.Now()
			c.UpdatedAt = c.CreatedAt
		}
		_, _ = store.Append(ctx, boardID, engine.OpCardAdd, engine.CardAddPayload{Card: c})
	}
}

// ── In-memory ObjectStore ───────────────────────────────────────────────
//
// Thin copy of the fakes used in the test suites. Duplicated here on
// purpose — this is a demo binary, not shared code.

type memObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemObjectStore() *memObjectStore {
	return &memObjectStore{objects: map[string][]byte{}}
}

func (m *memObjectStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = bytes.Clone(body)
	return nil
}

func (m *memObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return bytes.Clone(b), nil
}

func (m *memObjectStore) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memObjectStore) ListObjects(_ context.Context, prefix, startAfter string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if startAfter != "" && k <= startAfter {
			continue
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

func (m *memObjectStore) ListCommonPrefixes(_ context.Context, prefix, delimiter string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]struct{}{}
	for k := range m.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		idx := strings.Index(rest, delimiter)
		if idx < 0 {
			continue
		}
		seen[prefix+rest[:idx+1]] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}
