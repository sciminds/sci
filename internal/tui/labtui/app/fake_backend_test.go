package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"

	"github.com/sciminds/cli/internal/lab"
)

// fakeBackend is a deterministic Backend for tests. Listings are seeded by
// path; sizes are seeded as bytes per path; transfers replay a canned
// progress sequence.
type fakeBackend struct {
	mu sync.Mutex

	listings  map[string][]lab.Entry
	listErr   error
	listCalls []string

	sizes   map[string]int64 // per-path bytes
	sizeErr error

	progressFrames []lab.Progress
	transferErr    error
	transferCalls  []string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		listings: map[string][]lab.Entry{},
		sizes:    map[string]int64{},
	}
}

func (f *fakeBackend) seedListing(remotePath string, entries ...lab.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listings[path.Clean(remotePath)] = entries
}

func (f *fakeBackend) seedSize(remotePath string, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes[path.Clean(remotePath)] = n
}

func (f *fakeBackend) List(_ context.Context, remotePath string) ([]lab.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, remotePath)
	if f.listErr != nil {
		return nil, f.listErr
	}
	entries, ok := f.listings[path.Clean(remotePath)]
	if !ok {
		return nil, fmt.Errorf("fake: no listing seeded for %q", remotePath)
	}
	out := make([]lab.Entry, len(entries))
	copy(out, entries)
	return out, nil
}

func (f *fakeBackend) Size(_ context.Context, remotePaths []string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizeErr != nil {
		return 0, f.sizeErr
	}
	var total int64
	for _, p := range remotePaths {
		n, ok := f.sizes[path.Clean(p)]
		if !ok {
			return 0, fmt.Errorf("fake: no size seeded for %q", p)
		}
		total += n
	}
	return total, nil
}

func (f *fakeBackend) Transfer(ctx context.Context, remotePath, _ string, progress chan<- lab.Progress) error {
	f.mu.Lock()
	f.transferCalls = append(f.transferCalls, remotePath)
	frames := append([]lab.Progress(nil), f.progressFrames...)
	err := f.transferErr
	f.mu.Unlock()
	for _, p := range frames {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case progress <- p:
		}
	}
	return err
}

// errFake is a sentinel for transfer-failure tests.
var errFake = errors.New("fake transfer error")
