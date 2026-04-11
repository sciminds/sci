package app

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	engine "github.com/sciminds/cli/internal/board"
)

const (
	testTermW = 100
	testTermH = 30
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

// ── Fake ObjectStore ────────────────────────────────────────────────────

// fakeObjectStore is an in-memory implementation of engine.ObjectStore used
// in tests. Mirror of the one in internal/board; duplicated here because
// the board package's fake is unexported.
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}}
}

func (f *fakeObjectStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	f.objects[key] = cp
	return nil
}

func (f *fakeObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, nil
}

func (f *fakeObjectStore) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeObjectStore) ListObjects(_ context.Context, prefix, startAfter string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objects {
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

func (f *fakeObjectStore) ListCommonPrefixes(_ context.Context, prefix, delimiter string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	for k := range f.objects {
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

// ── Test setup helpers ──────────────────────────────────────────────────

// setupStore builds a board.Store backed by a temp SQLite cache and the
// in-memory fakeObjectStore, pre-populates it with a fixture board
// ("alpha") containing two columns and three cards, and returns the
// store ready for a Model to consume.
func setupStore(t *testing.T) *engine.Store {
	t.Helper()
	obj := newFakeObjectStore()
	cachePath := filepath.Join(t.TempDir(), "board.db")
	local, err := engine.OpenLocalCache(cachePath)
	if err != nil {
		t.Fatalf("open local cache: %v", err)
	}
	t.Cleanup(func() { _ = local.Close() })

	store := engine.NewStore(obj, local, "tester")
	ctx := context.Background()

	if err := store.CreateBoard(ctx, "alpha", "Alpha Board", "fixture",
		[]engine.Column{
			{ID: "todo", Title: "To Do"},
			{ID: "done", Title: "Done"},
		}); err != nil {
		t.Fatalf("create board: %v", err)
	}

	addCard := func(id, title, col string, pos float64) {
		c := engine.Card{ID: id, Title: title, Column: col, Position: pos}
		if _, err := store.Append(ctx, "alpha", engine.OpCardAdd, engine.CardAddPayload{Card: c}); err != nil {
			t.Fatalf("add card %s: %v", id, err)
		}
	}
	addCard("c1", "Write tests", "todo", 1.0)
	addCard("c2", "Ship it", "todo", 2.0)
	addCard("c3", "Done already", "done", 1.0)

	return store
}

// ── teatest helpers ─────────────────────────────────────────────────────

func sendKey(tm *teatest.TestModel, key string) {
	runes := []rune(key)
	if len(runes) == 1 {
		tm.Send(tea.KeyPressMsg{Code: runes[0], Text: key})
	} else {
		tm.Send(tea.KeyPressMsg{Text: key})
	}
}

func sendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

func waitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
}

func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	return final.(*Model)
}

func startTeatest(t *testing.T) *teatest.TestModel {
	t.Helper()
	store := setupStore(t)
	m := NewModel(store, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForOutput(t, tm, "alpha") // wait until ListBoards result renders
	return tm
}
