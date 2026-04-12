package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

// noteHandler is a minimal httptest fake scoped to the three
// note-related endpoints exercised by ListNoteChildren /
// CreateChildNote / UpdateChildNote. Kept separate from the shared
// itemHandler in items_test.go because those tests only need
// journalArticle semantics and the divergent bodies would tangle both
// handlers.
type noteHandler struct {
	t        *testing.T
	items    map[string]*noteItem // keyed by item key — both parents and children
	children map[string][]string  // parentKey → []childKey (in insertion order)
	nextKey  int32
	posts    int32
}

type noteItem struct {
	data    client.ItemData
	version int
}

func newNoteHandler(t *testing.T) *noteHandler {
	return &noteHandler{
		t:        t,
		items:    map[string]*noteItem{},
		children: map[string][]string{},
	}
}

// seedParent places a journalArticle item with the given key into the
// store so children can be attached to it. No notes yet.
func (h *noteHandler) seedParent(key string) {
	it := client.ItemData{ItemType: client.JournalArticle}
	k := key
	v := 1
	it.Key = &k
	it.Version = &v
	h.items[key] = &noteItem{data: it, version: 1}
}

// seedNoteChild attaches a note with the given body + tags to parentKey.
// Returns the assigned note key so tests can reference it.
func (h *noteHandler) seedNoteChild(parentKey, body string, tags ...string) string {
	h.nextKey++
	key := childKey(h.nextKey)
	nt := client.Note
	parent := parentKey
	data := client.ItemData{
		ItemType:   nt,
		Note:       &body,
		ParentItem: &parent,
	}
	if len(tags) > 0 {
		ts := make([]client.Tag, len(tags))
		for i, t := range tags {
			ts[i] = client.Tag{Tag: t}
		}
		data.Tags = &ts
	}
	k := key
	v := 1
	data.Key = &k
	data.Version = &v
	h.items[key] = &noteItem{data: data, version: 1}
	h.children[parentKey] = append(h.children[parentKey], key)
	return key
}

// seedAttachmentChild attaches an attachment item — used to verify the
// note filter on ListNoteChildren doesn't return non-note children.
func (h *noteHandler) seedAttachmentChild(parentKey string) string {
	h.nextKey++
	key := childKey(h.nextKey)
	at := client.Attachment
	parent := parentKey
	data := client.ItemData{
		ItemType:   at,
		ParentItem: &parent,
	}
	k := key
	v := 1
	data.Key = &k
	data.Version = &v
	h.items[key] = &noteItem{data: data, version: 1}
	h.children[parentKey] = append(h.children[parentKey], key)
	return key
}

// childKey deterministically maps a counter to an 8-char Zotero-style
// item key so tests can assert the concrete key value.
func childKey(n int32) string {
	// Key stable enough for assertion: CH000001, CH000002, ...
	return "CH" + pad6(int(n))
}

func pad6(n int) string {
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[:])
}

func (h *noteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/children"):
		// GET /users/42/items/{key}/children
		trimmed := strings.TrimSuffix(path, "/children")
		parentKey := strings.TrimPrefix(trimmed, "/users/42/items/")
		if _, ok := h.items[parentKey]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var out []client.Item
		for _, ck := range h.children[parentKey] {
			ci := h.items[ck]
			out = append(out, client.Item{
				Key:     ck,
				Version: ci.version,
				Data:    ci.data,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/users/42/items/"):
		// GET /users/42/items/{key} — needed by UpdateItem's version probe
		key := strings.TrimPrefix(path, "/users/42/items/")
		it, ok := h.items[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		wrapped := client.Item{Key: key, Version: it.version, Data: it.data}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wrapped)

	case r.Method == http.MethodPost && path == "/users/42/items":
		atomic.AddInt32(&h.posts, 1)
		body, _ := io.ReadAll(r.Body)
		var batch []client.ItemData
		if err := json.Unmarshal(body, &batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := map[string]any{
			"failed":     map[string]any{},
			"unchanged":  map[string]any{},
			"successful": map[string]any{},
		}
		for idx, d := range batch {
			// New-item path: no Key set
			h.nextKey++
			key := childKey(h.nextKey)
			k := key
			v := 1
			d.Key = &k
			d.Version = &v
			h.items[key] = &noteItem{data: d, version: 1}
			if d.ParentItem != nil {
				h.children[*d.ParentItem] = append(h.children[*d.ParentItem], key)
			}
			result["successful"].(map[string]any)[itoaIdx(idx)] = map[string]any{
				"key":     key,
				"version": 1,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)

	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/users/42/items/"):
		key := strings.TrimPrefix(path, "/users/42/items/")
		it, ok := h.items[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var patch client.ItemData
		if err := json.Unmarshal(body, &patch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if patch.Note != nil {
			it.data.Note = patch.Note
		}
		it.version++
		w.WriteHeader(http.StatusNoContent)

	default:
		h.t.Logf("unhandled: %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// TestListNoteChildren_FiltersToNotes verifies attachments and other
// non-note children are excluded from the result. The CKD paper's real
// library layout has exactly this mix (one PDF + potentially notes),
// so the filter is load-bearing.
func TestListNoteChildren_FiltersToNotes(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	h.seedParent("PARENT01")
	h.seedAttachmentChild("PARENT01")
	note1 := h.seedNoteChild("PARENT01", "<p>first note</p>", "docling")
	note2 := h.seedNoteChild("PARENT01", "<p>second note</p>")

	c, _ := newTestClient(t, h)
	got, err := c.ListNoteChildren(context.Background(), "PARENT01")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d notes, want 2 (attachment must be filtered): %+v", len(got), got)
	}
	byKey := map[string]NoteChild{}
	for _, nc := range got {
		byKey[nc.Key] = nc
	}
	if byKey[note1].Body != "<p>first note</p>" {
		t.Errorf("note1 body = %q", byKey[note1].Body)
	}
	if len(byKey[note1].Tags) != 1 || byKey[note1].Tags[0] != "docling" {
		t.Errorf("note1 tags = %v", byKey[note1].Tags)
	}
	if byKey[note2].Body != "<p>second note</p>" {
		t.Errorf("note2 body = %q", byKey[note2].Body)
	}
}

func TestListNoteChildren_EmptyParent(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	h.seedParent("P00")
	c, _ := newTestClient(t, h)
	got, err := c.ListNoteChildren(context.Background(), "P00")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d, want 0", len(got))
	}
}

func TestListNoteChildren_ParentNotFound(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	c, _ := newTestClient(t, h)
	_, err := c.ListNoteChildren(context.Background(), "NOPE0000")
	if err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestCreateChildNote_PostsWithParentAndTags(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	h.seedParent("PARENT01")

	c, _ := newTestClient(t, h)
	body := "<h1>Extracted</h1><p>...</p>"
	key, err := c.CreateChildNote(context.Background(), "PARENT01", body, []string{"docling"})
	if err != nil {
		t.Fatal(err)
	}
	// Verify the server-side state looks right.
	it, ok := h.items[key]
	if !ok {
		t.Fatalf("created key %q not in store", key)
	}
	if it.data.ItemType != client.Note {
		t.Errorf("itemType = %q, want note", it.data.ItemType)
	}
	if it.data.Note == nil || *it.data.Note != body {
		t.Errorf("note body not stored")
	}
	if it.data.ParentItem == nil || *it.data.ParentItem != "PARENT01" {
		t.Errorf("parentItem not set")
	}
	if it.data.Tags == nil || len(*it.data.Tags) != 1 || (*it.data.Tags)[0].Tag != "docling" {
		t.Errorf("tags not stored: %+v", it.data.Tags)
	}
	// Note must now appear as a child of PARENT01.
	found := false
	for _, ck := range h.children["PARENT01"] {
		if ck == key {
			found = true
		}
	}
	if !found {
		t.Errorf("new note %q not listed as child of PARENT01", key)
	}
}

func TestCreateChildNote_NoTags(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	h.seedParent("PARENT01")
	c, _ := newTestClient(t, h)
	key, err := c.CreateChildNote(context.Background(), "PARENT01", "<p>x</p>", nil)
	if err != nil {
		t.Fatal(err)
	}
	it := h.items[key]
	if it.data.Tags != nil && len(*it.data.Tags) > 0 {
		t.Errorf("expected no tags, got %+v", it.data.Tags)
	}
}

func TestUpdateChildNote_PatchesBodyInPlace(t *testing.T) {
	t.Parallel()
	h := newNoteHandler(t)
	h.seedParent("PARENT01")
	noteKey := h.seedNoteChild("PARENT01", "<p>old</p>", "docling")
	origVersion := h.items[noteKey].version

	c, _ := newTestClient(t, h)
	if err := c.UpdateChildNote(context.Background(), noteKey, "<p>new</p>"); err != nil {
		t.Fatal(err)
	}
	it := h.items[noteKey]
	if it.data.Note == nil || *it.data.Note != "<p>new</p>" {
		t.Errorf("body not updated: %v", it.data.Note)
	}
	if it.version <= origVersion {
		t.Errorf("version not bumped: %d → %d", origVersion, it.version)
	}
	// Parent relationship must be preserved (PATCH in place, not recreate).
	if it.data.ParentItem == nil || *it.data.ParentItem != "PARENT01" {
		t.Errorf("parent relationship lost on update")
	}
}
