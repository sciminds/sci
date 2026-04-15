// Package app implements the lab storage browser TUI: drill-down folder
// navigation, multi-select, size confirmation, and resumable rsync transfer
// with a progress bar.
package app

import (
	"context"

	"github.com/sciminds/cli/internal/lab"
)

// Backend abstracts the network-side operations the TUI performs so tests
// can substitute a fake. Real implementation in ssh_backend.go shells out
// to ssh + rsync; tests use a fake with canned responses.
type Backend interface {
	// List returns the entries at remotePath. Should be context-cancellable
	// because slow ssh listings shouldn't block the TUI on quit.
	List(ctx context.Context, remotePath string) ([]lab.Entry, error)
	// Size returns total bytes consumed by remotePaths (one ssh call,
	// du -sbc semantics).
	Size(ctx context.Context, remotePaths []string) (int64, error)
	// Transfer downloads remotePath into localDir, sending one Progress
	// per --info=progress2 line on progress. Blocks until rsync exits.
	Transfer(ctx context.Context, remotePath, localDir string, progress chan<- lab.Progress) error
}
