package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/uikit"
)

// pollInterval is how often the TUI asks Store.Poll for new remote events.
// Exported as a var so tests can shorten it.
var pollInterval = 30 * time.Second

// cmdTimeout caps how long a single tea.Cmd waits on the store before
// returning an error. Bubble Tea's event loop expects cmds to return
// promptly; long network operations must not wedge the UI.
var cmdTimeout = 10 * time.Second

func listBoardsCmd(store *engine.Store) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), cmdTimeout, func(ctx context.Context) ([]string, error) {
		return store.ListBoards(ctx)
	})
}

func loadBoardCmd(store *engine.Store, id string) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), cmdTimeout, func(ctx context.Context) (engine.Board, error) {
		return store.Load(ctx, id)
	})
}

// AppendCmd queues and uploads a single event. The optimistic update is
// applied to the Model *before* this cmd runs — see update.go — so the
// caller never waits on this to see its own edit.
//
// Exposed (uppercase) because no edit UX wires it yet; keeping it public
// signals it as the entry point the first edit operation should reach for.
func AppendCmd(store *engine.Store, boardID string, op engine.Op, payload any) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), cmdTimeout, func(ctx context.Context) (struct{}, error) {
		_, err := store.Append(ctx, boardID, op, payload)
		return struct{}{}, err
	})
}

// pollCmd runs Store.Poll once and schedules itself again via a
// tea.Tick. The Model re-issues this cmd on start.
func pollCmd(store *engine.Store, boardID, sinceID string) tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		ids, err := store.Poll(ctx, boardID, sinceID)
		return pollMsg{newIDs: ids, err: err}
	})
}
