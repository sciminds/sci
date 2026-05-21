package app

// provider.go — browser.Provider implementation backed by an in-memory
// snapshot of cloud.ObjectInfo. Children() filters the snapshot by
// prefix via share.ChildrenAt; Remove() prunes a key so the next
// refresh reflects a successful delete.
//
// The provider owns its own mutex because actions mutate Objects from
// goroutines spawned by their tea.Cmds. Locking is fine-grained — the
// listing fetch is in-memory and microseconds-fast.

import (
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// Provider implements browser.Provider over a mutable []cloud.ObjectInfo.
type Provider struct {
	mu      sync.Mutex
	objects []cloud.ObjectInfo
	client  *cloud.Client
}

// NewProvider wraps the listing. The returned Provider owns the slice
// (callers must not mutate it externally after this point).
func NewProvider(objects []cloud.ObjectInfo, client *cloud.Client) *Provider {
	return &Provider{objects: objects, client: client}
}

// Children returns a tea.Cmd that snapshots the listing and emits a
// browser.ChildrenMsg with the immediate children at path.
func (p *Provider) Children(path string) tea.Cmd {
	return func() tea.Msg {
		p.mu.Lock()
		entries := share.ChildrenAt(p.objects, path)
		p.mu.Unlock()
		items := lo.Map(entries, func(e share.TreeEntry, _ int) browser.Entry {
			return Entry{T: e}
		})
		return browser.ChildrenMsg{Path: path, Entries: items}
	}
}

// Root is the bucket root ("").
func (*Provider) Root() string { return "" }

// Parent returns the parent path; "" stays "".
func (*Provider) Parent(path string) string { return share.ParentPath(path) }

// Breadcrumb renders the title bar as "sciminds/<bucket> / a / b / c".
// Mirrors the old listtui behavior.
func (p *Provider) Breadcrumb(path string) string {
	base := "cloud"
	if p.client != nil {
		base = "sciminds/" + p.client.Bucket
	}
	if path == "" {
		return base
	}
	return base + " / " + strings.ReplaceAll(path, "/", " / ")
}

// Remove drops every object whose Key equals fullKey. Called by the
// delete action *after* the network call succeeds; the subsequent
// browser.RefreshMsg re-runs Children() against the pruned slice.
func (p *Provider) Remove(fullKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.objects = lo.Filter(p.objects, func(o cloud.ObjectInfo, _ int) bool {
		return o.Key != fullKey
	})
}

// RemovePrefix drops every object whose Key starts with fullPrefix+"/".
// Called by the folder-delete action after a successful recursive
// remove. The trailing slash guards against substring matches —
// e.g. RemovePrefix("ejolly/py") won't prune "ejolly/pyproject.toml".
func (p *Provider) RemovePrefix(fullPrefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	guard := fullPrefix + "/"
	p.objects = lo.Filter(p.objects, func(o cloud.ObjectInfo, _ int) bool {
		return !strings.HasPrefix(o.Key, guard)
	})
}

// Objects returns a snapshot of the underlying slice. Used by tests
// asserting on the post-delete state.
func (p *Provider) Objects() []cloud.ObjectInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.objects)
}

// Client returns the underlying cloud.Client; actions need it for
// ownership checks and async commands.
func (p *Provider) Client() *cloud.Client { return p.client }
