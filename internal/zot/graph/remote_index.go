package graph

import (
	"context"
	"strings"
	"sync"

	"github.com/sciminds/cli/internal/zot/api"
)

// remoteIndex is a LibraryIndex backed by api.Client.ListItems. It
// pre-fetches the entire library on first use and caches a
// DOI(lower) → key map for the lifetime of the value. One graph command
// invocation = one fetch + one intersection — cheap for libraries up to
// a few thousand items.
//
// For huge libraries the pre-fetch becomes the dominant cost (Zotero
// caps pages at 100, so a 5000-item library = 50 round trips). Worth
// revisiting then; for now this matches the agent use case where
// in_library accuracy beats first-call latency.
type remoteIndex struct {
	c    *api.Client
	once sync.Once
	doiK map[string]string
	err  error
	ctx  context.Context
}

// RemoteIndex returns a LibraryIndex that pulls every item in the
// configured library via api.Client.ListItems and serves DOI lookups
// from the resulting in-memory map. The context is captured for the
// lazy fetch — make sure to pass one with a sensible timeout.
func RemoteIndex(ctx context.Context, c *api.Client) LibraryIndex {
	return &remoteIndex{c: c, ctx: ctx}
}

func (r *remoteIndex) LookupKeysByDOI(dois []string) (map[string]string, error) {
	r.once.Do(r.warm)
	if r.err != nil {
		return nil, r.err
	}
	out := map[string]string{}
	for _, d := range dois {
		k := strings.ToLower(strings.TrimSpace(d))
		if v, ok := r.doiK[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (r *remoteIndex) warm() {
	items, err := r.c.ListItems(r.ctx, api.ListItemsOptions{})
	if err != nil {
		r.err = err
		return
	}
	r.doiK = map[string]string{}
	for _, it := range items {
		if it.Data.DOI == nil || *it.Data.DOI == "" {
			continue
		}
		r.doiK[strings.ToLower(strings.TrimSpace(*it.Data.DOI))] = it.Key
	}
}
