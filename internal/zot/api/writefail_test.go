package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TestCreateChildNote_TooLongFailedSlot: Zotero rejects an oversize note
// with HTTP 200 + a failed slot (code 413, "Note '…' too long"). The
// error must surface as a *WriteFailedError whose NoteTooLong() is true,
// so callers (extract-lib) can stop retrying a body the server will
// never accept.
func TestCreateChildNote_TooLongFailedSlot(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"successful": {}, "success": {}, "unchanged": {},
			"failed": {"0": {"code": 413, "message": "Note '<h1>Long paper</h1>...' too long for item ABCD1234"}}
		}`))
	})
	c, _ := newTestClient(t, h)

	_, err := c.CreateChildNote(context.Background(), "PARENT01", "<p>huge</p>", nil)
	if err == nil {
		t.Fatal("expected error for failed slot")
	}
	wf, ok := errors.AsType[*WriteFailedError](err)
	if !ok {
		t.Fatalf("error %v (%T) is not a *WriteFailedError", err, err)
	}
	if wf.Code != 413 {
		t.Errorf("Code = %d, want 413", wf.Code)
	}
	if !wf.NoteTooLong() {
		t.Error("NoteTooLong() = false, want true")
	}
}

// TestCreateChildNote_HTTP413: some oversize payloads are rejected at
// the HTTP layer (413 on the whole request) before Zotero produces a
// failed slot. That must classify the same way.
func TestCreateChildNote_HTTP413(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
	})
	c, _ := newTestClient(t, h)

	_, err := c.CreateChildNote(context.Background(), "PARENT01", "<p>huge</p>", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 413")
	}
	wf, ok := errors.AsType[*WriteFailedError](err)
	if !ok {
		t.Fatalf("error %v (%T) is not a *WriteFailedError", err, err)
	}
	if !wf.NoteTooLong() {
		t.Error("NoteTooLong() = false, want true")
	}
}

// TestWriteFailedError_NoteTooLong pins the classification rule: code
// 413 or a "too long" message means the server will never accept this
// body; anything else stays retriable.
func TestWriteFailedError_NoteTooLong(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  WriteFailedError
		want bool
	}{
		{"code 413, no message", WriteFailedError{Code: 413}, true},
		{"message only", WriteFailedError{Code: 400, Message: "Note '<p>x</p>' Too Long"}, true},
		{"unrelated 400", WriteFailedError{Code: 400, Message: "Invalid value for itemType"}, false},
		{"unrelated 500", WriteFailedError{Code: 500, Message: "internal error"}, false},
	}
	for _, tc := range cases {
		if got := tc.err.NoteTooLong(); got != tc.want {
			t.Errorf("%s: NoteTooLong() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
