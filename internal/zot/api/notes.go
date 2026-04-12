package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sciminds/cli/internal/zot/client"
)

// ChildItem is the minimal projection of a child item (note OR
// attachment — any type) used by read-side commands like
// `zot item children`. Mixed fields are populated based on ItemType:
//
//   - ItemType="note":       Note body set, Filename/ContentType empty
//   - ItemType="attachment": Filename + ContentType set, Note empty
//
// Title is the attachment/note's own title field (may be empty for
// notes that don't have one set).
type ChildItem struct {
	Key         string   `json:"key"`
	ItemType    string   `json:"item_type"`
	Title       string   `json:"title,omitempty"`
	Note        string   `json:"note,omitempty"`         // body, notes only
	ContentType string   `json:"content_type,omitempty"` // attachments only
	Filename    string   `json:"filename,omitempty"`     // attachments only
	Tags        []string `json:"tags,omitempty"`
}

// ListChildren returns every child of parentKey — notes, attachments,
// and anything else Zotero supports. Unlike ListNoteChildren, this
// does NOT filter by itemType; callers that only want notes should
// prefer ListNoteChildren for the narrower typed return.
func (c *Client) ListChildren(ctx context.Context, parentKey string) ([]ChildItem, error) {
	resp, err := c.Gen.GetItemChildrenWithResponse(ctx, c.UserID, client.ItemKeyPath(parentKey), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("parent item %s not found", parentKey)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GET /items/%s/children: %s: %s", parentKey, resp.Status(), string(resp.Body))
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	out := make([]ChildItem, 0, len(*resp.JSON200))
	for _, it := range *resp.JSON200 {
		ci := ChildItem{
			Key:      it.Key,
			ItemType: string(it.Data.ItemType),
		}
		if it.Data.Title != nil {
			ci.Title = *it.Data.Title
		}
		if it.Data.Note != nil {
			ci.Note = *it.Data.Note
		}
		if it.Data.ContentType != nil {
			ci.ContentType = *it.Data.ContentType
		}
		if it.Data.Filename != nil {
			// Zotero stores attachment paths as "storage:<name>"
			// in some contexts; the Filename field is already
			// stripped but we defensively trim anyway.
			ci.Filename = strings.TrimPrefix(*it.Data.Filename, "storage:")
		}
		if it.Data.Tags != nil {
			for _, t := range *it.Data.Tags {
				ci.Tags = append(ci.Tags, t.Tag)
			}
		}
		out = append(out, ci)
	}
	return out, nil
}

// NoteChild is the minimum projection of a note item that higher
// layers (extract, fix, cli) consume. Defined here so callers don't
// have to know about the generated client.Item surface.
type NoteChild struct {
	Key  string   // Zotero 8-char item key of the note
	Body string   // HTML body (may be empty for a brand-new note)
	Tags []string // flat list of tag names, or nil
}

// ListNoteChildren returns the note children of parentKey. Attachments
// and other non-note children are filtered out — the Zotero Web API
// has no server-side itemType filter on `/items/{key}/children`, so
// filtering happens in Go.
//
// Returns an error (not an empty list) when the parent itself doesn't
// exist, so callers can tell "no notes yet" apart from "wrong key".
func (c *Client) ListNoteChildren(ctx context.Context, parentKey string) ([]NoteChild, error) {
	resp, err := c.Gen.GetItemChildrenWithResponse(ctx, c.UserID, client.ItemKeyPath(parentKey), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("parent item %s not found", parentKey)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GET /items/%s/children: %s: %s", parentKey, resp.Status(), string(resp.Body))
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	var out []NoteChild
	for _, it := range *resp.JSON200 {
		if it.Data.ItemType != client.Note {
			continue
		}
		nc := NoteChild{Key: it.Key}
		if it.Data.Note != nil {
			nc.Body = *it.Data.Note
		}
		if it.Data.Tags != nil {
			for _, t := range *it.Data.Tags {
				nc.Tags = append(nc.Tags, t.Tag)
			}
		}
		out = append(out, nc)
	}
	return out, nil
}

// CreateChildNote creates a new note item attached to parentKey.
// htmlBody is the note body (Zotero renders HTML in the note pane);
// tags is optional and applied at create time so the note is
// immediately greppable via tag-based dedupe.
//
// Returns the server-assigned 8-char note key.
func (c *Client) CreateChildNote(ctx context.Context, parentKey, htmlBody string, tags []string) (string, error) {
	data := client.ItemData{
		ItemType:   client.Note,
		Note:       &htmlBody,
		ParentItem: &parentKey,
	}
	if len(tags) > 0 {
		ts := make([]client.Tag, len(tags))
		for i, t := range tags {
			ts[i] = client.Tag{Tag: t}
		}
		data.Tags = &ts
	}
	return c.CreateItem(ctx, data)
}

// UpdateChildNote replaces an existing note's HTML body in place.
// PATCH-in-place preserves the note's key, parent relationship, tags,
// and Zotero's internal history — no trash, no delete. Handles 412
// Precondition Failed via the shared UpdateItem version-retry path.
func (c *Client) UpdateChildNote(ctx context.Context, noteKey, htmlBody string) error {
	return c.UpdateItem(ctx, noteKey, client.ItemData{
		ItemType: client.Note,
		Note:     &htmlBody,
	})
}
