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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	engine "github.com/sciminds/cli/internal/board"
	btui "github.com/sciminds/cli/internal/tui/board"
)

func main() {
	store, err := seededStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}

	if err := btui.Run(store, ""); err != nil {
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

	// ── Board 1: "triage" with a realistic spread of cards.
	if err := store.CreateBoard(ctx, "triage", "Triage", "inbound bugs & ideas",
		[]engine.Column{
			{ID: "inbox", Title: "Inbox"},
			{ID: "next", Title: "Up Next", WIP: 3},
			{ID: "doing", Title: "In Progress", WIP: 2},
			{ID: "done", Title: "Done"},
		}); err != nil {
		return nil, err
	}
	due := time.Now().Add(72 * time.Hour)
	seedCards(ctx, store, "triage", []engine.Card{
		{ID: "c1", Title: "Investigate flaky rollup test", Column: "inbox", Position: 1, Priority: "high", Labels: []string{"test", "ci"}},
		{ID: "c2", Title: "Migrate legacy auth tokens", Column: "inbox", Position: 2, Priority: "med", Labels: []string{"auth"}},
		{ID: "c3", Title: "Rename 'publish' → 'release'", Column: "inbox", Position: 3},
		{ID: "c4", Title: "Add --dry-run to sync command", Column: "next", Position: 1, Priority: "med", Labels: []string{"dx"}, DueDate: &due},
		{ID: "c5", Title: "Fix empty-column rendering", Column: "next", Position: 2, Priority: "low", Assignees: []string{"alice"}},
		{ID: "c6", Title: "Prototype kanban TUI", Column: "doing", Position: 1, Priority: "high", Labels: []string{"ui", "research"},
			Description: "scaffold the bubbletea side with three screens, optimistic updates, and polling.",
			Checklist: []engine.ChecklistItem{
				{ID: "l1", Text: "Picker screen", Done: true},
				{ID: "l2", Text: "Grid screen", Done: true},
				{ID: "l3", Text: "Detail screen", Done: true},
				{ID: "l4", Text: "Edit UX", Done: false},
				{ID: "l5", Text: "Snapshot auto-trigger", Done: false},
			},
			Comments: []engine.Comment{
				{ID: "cm1", Author: "demo", Text: "split into ui/ and app/ to keep styles isolated", Ts: time.Now().Add(-2 * time.Hour)},
				{ID: "cm2", Author: "demo", Text: "ctrl+c must always quit — learned the hard way", Ts: time.Now().Add(-1 * time.Hour)},
			},
		},
		{ID: "c7", Title: "Write teatests for poll cadence", Column: "doing", Position: 2, Priority: "low", Assignees: []string{"demo"}},
		{ID: "c8", Title: "Ship headless engine", Column: "done", Position: 1, Labels: []string{"done"}},
		{ID: "c9", Title: "Wire CloudAdapter + live smoke", Column: "done", Position: 2, Labels: []string{"done"}},
		{ID: "c10", Title: "Auto-migrate legacy cfg.Public", Column: "done", Position: 3, Labels: []string{"done"}},
	})

	// ── Board 2: "roadmap" — fewer cards, different shape.
	if err := store.CreateBoard(ctx, "roadmap", "Q3 Roadmap", "quarterly goals",
		[]engine.Column{
			{ID: "backlog", Title: "Backlog"},
			{ID: "comm", Title: "Committed"},
			{ID: "shipped", Title: "Shipped"},
		}); err != nil {
		return nil, err
	}
	seedCards(ctx, store, "roadmap", []engine.Card{
		{ID: "r1", Title: "Shared kanban for lab", Column: "comm", Position: 1, Priority: "high"},
		{ID: "r2", Title: "Zotero hygiene checks", Column: "shipped", Position: 1},
		{ID: "r3", Title: "Marimo integration", Column: "backlog", Position: 1, Priority: "low"},
	})

	return store, nil
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
	cp := make([]byte, len(body))
	copy(cp, body)
	m.objects[key] = cp
	return nil
}

func (m *memObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, nil
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
	sort.Strings(out)
	return out, nil
}

func (m *memObjectStore) ListCommonPrefixes(_ context.Context, prefix, delimiter string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for k := range m.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		idx := strings.Index(rest, delimiter)
		if idx < 0 {
			continue
		}
		seen[prefix+rest[:idx+1]] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
