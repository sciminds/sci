package api

import (
	"context"
	"io"

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
	CreateItem(ctx context.Context, data client.ItemData) (*client.Item, error)
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

	// Attachments — 4-phase upload dance (see files.go). CreateChildAttachment
	// is phase 1 (item creation); UploadAttachmentFile drives phases 2→4
	// (authorization → S3 → register) with a dedup short-circuit.
	CreateChildAttachment(ctx context.Context, parentKey string, meta AttachmentMeta) (string, error)
	UploadAttachmentFile(ctx context.Context, itemKey string, r io.Reader, filename, contentType string) error

	// Collections
	CreateCollection(ctx context.Context, name, parentKey string) (*client.Collection, error)
	DeleteCollection(ctx context.Context, key string) error

	// Saved searches
	CreateSavedSearch(ctx context.Context, name string, conditions []client.SearchCondition) (*client.Search, error)
	UpdateSavedSearch(ctx context.Context, key, name string, conditions []client.SearchCondition) error
	DeleteSavedSearch(ctx context.Context, key string) error

	// Tags
	DeleteTagsFromLibrary(ctx context.Context, tags []string) error
}

// Compile-time assertion: *Client satisfies Writer.
var _ Writer = (*Client)(nil)
