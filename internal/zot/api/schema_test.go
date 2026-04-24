package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
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
