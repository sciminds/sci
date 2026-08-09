package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/samber/lo"
)

// itemTemplateHandler serves GET /items/new for tests.
type itemTemplateHandler struct {
	mu       sync.Mutex
	gotPath  string
	gotQuery map[string]string
	status   int // 0 → 200
	body     []byte
}

func (h *itemTemplateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	h.gotPath = r.URL.Path
	h.gotQuery = map[string]string{
		"itemType": r.URL.Query().Get("itemType"),
		"linkMode": r.URL.Query().Get("linkMode"),
	}
	status := h.status
	body := h.body
	h.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

// schemaHandler serves /itemTypeFields and /itemTypeCreatorTypes from a
// trimmed copy of what the live API returns, counting requests per path so
// the caching contract can be asserted.
type schemaHandler struct {
	mu     sync.Mutex
	n      map[string]int
	status int // 0 → 200

	fields   map[string][]string
	creators map[string][]string
}

func newSchemaHandler() *schemaHandler {
	return &schemaHandler{
		n: map[string]int{},
		fields: map[string][]string{
			"journalArticle":  {"title", "abstractNote", "publicationTitle", "volume", "issue", "pages", "DOI", "extra"},
			"bookSection":     {"title", "abstractNote", "bookTitle", "edition", "date", "publisher", "place", "pages", "ISBN", "extra"},
			"conferencePaper": {"title", "abstractNote", "proceedingsTitle", "conferenceName", "publisher", "place", "pages", "extra"},
			"book":            {"title", "abstractNote", "edition", "date", "publisher", "place", "numPages", "ISBN", "extra"},
		},
		creators: map[string][]string{
			"journalArticle":  {"author", "contributor", "editor", "reviewedAuthor", "translator"},
			"bookSection":     {"author", "bookAuthor", "contributor", "editor", "seriesEditor", "translator"},
			"conferencePaper": {"author", "contributor", "editor", "seriesEditor", "translator"},
			"book":            {"author", "contributor", "editor", "seriesEditor", "translator"},
		},
	}
}

func (h *schemaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.n[r.URL.Path]++
	status := h.status
	h.mu.Unlock()

	if status != 0 {
		w.WriteHeader(status)
		return
	}
	itemType := r.URL.Query().Get("itemType")
	var names []string
	var key string
	switch r.URL.Path {
	case "/itemTypeFields":
		names, key = h.fields[itemType], "field"
	case "/itemTypeCreatorTypes":
		names, key = h.creators[itemType], "creatorType"
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	rows := lo.Map(names, func(n string, _ int) map[string]string {
		return map[string]string{key: n, "localized": n}
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (h *schemaHandler) calls(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n[path]
}

func TestItemTemplate_Success(t *testing.T) {
	t.Parallel()
	h := &itemTemplateHandler{
		body: []byte(`{"itemType":"journalArticle","title":"","creators":[],"tags":[]}`),
	}
	c, _ := newTestClient(t, h)

	data, err := c.ItemTemplate(context.Background(), "journalArticle", "")
	if err != nil {
		t.Fatal(err)
	}
	if data == nil {
		t.Fatal("data = nil")
	}
	if string(data.ItemType) != "journalArticle" {
		t.Errorf("ItemType = %q, want journalArticle", data.ItemType)
	}
	if h.gotPath != "/items/new" {
		t.Errorf("path = %q, want /items/new", h.gotPath)
	}
	if h.gotQuery["itemType"] != "journalArticle" {
		t.Errorf("itemType = %q, want journalArticle", h.gotQuery["itemType"])
	}
	if h.gotQuery["linkMode"] != "" {
		t.Errorf("linkMode = %q, want empty for non-attachment", h.gotQuery["linkMode"])
	}
}

func TestItemTemplate_AttachmentWithLinkMode(t *testing.T) {
	t.Parallel()
	h := &itemTemplateHandler{
		body: []byte(`{"itemType":"attachment","linkMode":"imported_file","title":"","url":""}`),
	}
	c, _ := newTestClient(t, h)

	_, err := c.ItemTemplate(context.Background(), "attachment", "imported_file")
	if err != nil {
		t.Fatal(err)
	}
	if h.gotQuery["itemType"] != "attachment" {
		t.Errorf("itemType = %q, want attachment", h.gotQuery["itemType"])
	}
	if h.gotQuery["linkMode"] != "imported_file" {
		t.Errorf("linkMode = %q, want imported_file", h.gotQuery["linkMode"])
	}
}

func TestItemTemplate_BadType(t *testing.T) {
	t.Parallel()
	h := &itemTemplateHandler{status: http.StatusBadRequest}
	c, _ := newTestClient(t, h)

	_, err := c.ItemTemplate(context.Background(), "notAType", "")
	if err == nil {
		t.Fatal("expected error on 400")
	}
}
