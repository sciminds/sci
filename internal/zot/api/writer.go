package api

import (
	"context"

	"github.com/sciminds/cli/internal/zot/client"
)

// Writer is the write-only contract for the Zotero Web API.
//
// Every method mutates the remote library via HTTP. Read operations
// (version fetches for 412 retry, etc.) are internal implementation
// details of *Client and deliberately excluded from this interface.
//
// Local reads go through local.Reader; remote writes go through Writer.
// Together they enforce the "reads local, writes cloud" firewall.
//
// Do NOT add read methods to this interface. If you need to query
// library state, use local.Reader against the local zotero.sqlite.
type Writer interface {
	// Items
	CreateItem(ctx context.Context, data client.ItemData) (string, error)
	UpdateItem(ctx context.Context, key string, patch client.ItemData) error
	UpdateItemsBatch(ctx context.Context, patches []ItemPatch) (map[string]error, error)
	TrashItem(ctx context.Context, key string) error

	// Item membership
	AddItemToCollection(ctx context.Context, itemKey, collKey string) error
	RemoveItemFromCollection(ctx context.Context, itemKey, collKey string) error
	AddTagToItem(ctx context.Context, itemKey, tag string) error
	RemoveTagFromItem(ctx context.Context, itemKey, tag string) error

	// Notes
	CreateChildNote(ctx context.Context, parentKey, htmlBody string, tags []string) (string, error)
	UpdateChildNote(ctx context.Context, noteKey, htmlBody string) error

	// Collections
	CreateCollection(ctx context.Context, name, parentKey string) (string, error)
	DeleteCollection(ctx context.Context, key string) error

	// Tags
	DeleteTagsFromLibrary(ctx context.Context, tags []string) error
}

// Compile-time assertion: *Client satisfies Writer.
var _ Writer = (*Client)(nil)
