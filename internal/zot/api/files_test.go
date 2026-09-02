package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// uploadFake wraps fakeZotero with the one route it does not know: the
// per-item /file endpoint that phases 2 and 4 of an upload POST to. Every
// other request is delegated to the wrapped fake.
type uploadFake struct {
	zot *fakeZotero
	// s3URL is what the phase-2 authorization points the client at.
	s3URL string
	// uploadKey is handed out in phase 2 and expected back in phase 4.
	uploadKey string
	// exists makes phase 2 answer the dedup short-circuit.
	exists bool
	// phase2Status, when nonzero, is answered to phase 2 instead of an
	// authorization.
	phase2Status int

	mu   sync.Mutex
	file []recordedRequest
}

const (
	fakeS3ContentType = "multipart/form-data; boundary=abc123"
	fakeS3Prefix      = "--abc123\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\n"
	fakeS3Suffix      = "\r\n--abc123--\r\n"
)

func newUploadFake(t *testing.T, s3URL string) *uploadFake {
	t.Helper()
	return &uploadFake{zot: newFakeZotero(t), s3URL: s3URL, uploadKey: "UPLOADKEY001"}
}

// fileRequests returns the /file requests seen, in order.
func (u *uploadFake) fileRequests() []recordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.file)
}

func (u *uploadFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/file") {
		u.zot.ServeHTTP(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.file = append(u.file, recordedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: string(body)})
	u.mu.Unlock()

	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	if form.Has("upload") { // phase 4
		if form.Get("upload") != u.uploadKey {
			http.Error(w, "unknown upload key", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// phase 2
	switch {
	case u.phase2Status != 0:
		http.Error(w, "phase 2 refused", u.phase2Status)
	case u.exists:
		writeJSON(w, http.StatusOK, map[string]any{"exists": 1})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"url":         u.s3URL,
			"contentType": fakeS3ContentType,
			"prefix":      fakeS3Prefix,
			"suffix":      fakeS3Suffix,
			"uploadKey":   u.uploadKey,
			"params":      map[string]any{},
		})
	}
}

// fakeS3 is the pre-signed upload target of phase 3. It is a real loopback
// server, not the in-memory test network, because phase 3 deliberately goes
// through a plain http.Client that has no access to the fake network.
type fakeS3 struct {
	status string
	code   int

	mu   sync.Mutex
	seen []recordedRequest
}

func newFakeS3(t *testing.T) (*fakeS3, string) {
	t.Helper()
	s := &fakeS3{code: http.StatusNoContent}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv.URL + "/bucket"
}

func (s *fakeS3) requests() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.seen)
}

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, recordedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: string(body)})
	s.mu.Unlock()
	w.WriteHeader(s.code)
	_, _ = io.WriteString(w, s.status)
}

func newUploadClient(t *testing.T, s3URL string) (*Client, *uploadFake) {
	t.Helper()
	f := newUploadFake(t, s3URL)
	c, _ := newTestClient(t, f)
	return c, f
}

func TestCreateChildAttachment_PostsImportedFileUnderParent(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	got, err := c.CreateChildAttachment(context.Background(), "PARENT01", AttachmentMeta{
		Filename: "paper.pdf", ContentType: "application/pdf", Title: "Full Text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Key == "" {
		t.Fatal("created attachment has no key")
	}
	var body []map[string]any
	if err := json.Unmarshal([]byte(f.seen()[0].Body), &body); err != nil || len(body) != 1 {
		t.Fatalf("body = %s, want a one-element array", f.seen()[0].Body)
	}
	want := map[string]any{
		"itemType": "attachment", "linkMode": "imported_file", "parentItem": "PARENT01",
		"filename": "paper.pdf", "contentType": "application/pdf", "title": "Full Text",
	}
	for k, v := range want {
		if body[0][k] != v {
			t.Errorf("data[%q] = %v, want %v", k, body[0][k], v)
		}
	}
}

func TestCreateChildAttachment_OmitsEmptyTitle(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	if _, err := c.CreateChildAttachment(context.Background(), "PARENT01", AttachmentMeta{
		Filename: "paper.pdf", ContentType: "application/pdf",
	}); err != nil {
		t.Fatal(err)
	}
	var body []map[string]any
	if err := json.Unmarshal([]byte(f.seen()[0].Body), &body); err != nil || len(body) != 1 {
		t.Fatalf("body = %s, want a one-element array", f.seen()[0].Body)
	}
	if _, ok := body[0]["title"]; ok {
		t.Errorf("data carries title %v; an empty title must be left out so Zotero derives one", body[0]["title"])
	}
}

// TestUploadAttachmentFile_FourPhases walks the whole dance and asserts
// what each phase carried. Not parallel: it pins the clock through the
// package-level now.
func TestUploadAttachmentFile_FourPhases(t *testing.T) {
	fixed := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	prev := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = prev })

	s3, s3URL := newFakeS3(t)
	c, f := newUploadClient(t, s3URL)
	content := []byte("%PDF-1.4 fake bytes")

	uploaded, err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader(string(content)), "paper.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Error("uploaded = false, want true after a full upload")
	}

	file := f.fileRequests()
	if len(file) != 2 {
		t.Fatalf("saw %d /file requests, want 2 (authorize + register): %+v", len(file), file)
	}
	// Phase 2: authorization request.
	auth := file[0]
	if auth.Method != http.MethodPost || auth.Path != "/users/42/items/ATTACH01/file" {
		t.Errorf("phase 2 = %s %s, want POST /users/42/items/ATTACH01/file", auth.Method, auth.Path)
	}
	if got := auth.Header.Get("If-None-Match"); got != "*" {
		t.Errorf("phase 2 If-None-Match = %q, want *", got)
	}
	if got := auth.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("phase 2 Content-Type = %q, want form-urlencoded", got)
	}
	if got := auth.Header.Get("Zotero-API-Key"); got != "test-key" {
		t.Errorf("phase 2 Zotero-API-Key = %q, want test-key", got)
	}
	form, err := url.ParseQuery(auth.Body)
	if err != nil {
		t.Fatalf("phase 2 body %q is not a form: %v", auth.Body, err)
	}
	sum := md5.Sum(content)
	wantForm := map[string]string{
		"md5":      hex.EncodeToString(sum[:]),
		"filename": "paper.pdf",
		"filesize": strconv.Itoa(len(content)),
		"mtime":    strconv.FormatInt(fixed.UnixMilli(), 10),
	}
	for k, v := range wantForm {
		if form.Get(k) != v {
			t.Errorf("phase 2 form[%s] = %q, want %q", k, form.Get(k), v)
		}
	}

	// Phase 3: the S3 POST.
	s3Reqs := s3.requests()
	if len(s3Reqs) != 1 {
		t.Fatalf("S3 saw %d requests, want 1", len(s3Reqs))
	}
	if s3Reqs[0].Method != http.MethodPost || s3Reqs[0].Path != "/bucket" {
		t.Errorf("phase 3 = %s %s, want POST /bucket", s3Reqs[0].Method, s3Reqs[0].Path)
	}
	if got := s3Reqs[0].Header.Get("Content-Type"); got != fakeS3ContentType {
		t.Errorf("phase 3 Content-Type = %q, want the authorization's %q", got, fakeS3ContentType)
	}
	if want := fakeS3Prefix + string(content) + fakeS3Suffix; s3Reqs[0].Body != want {
		t.Errorf("phase 3 body = %q, want prefix+bytes+suffix %q", s3Reqs[0].Body, want)
	}
	for _, h := range []string{"Zotero-API-Key", "Zotero-API-Version"} {
		if got := s3Reqs[0].Header.Get(h); got != "" {
			t.Errorf("phase 3 carries %s=%q; the pre-signed POST must not see Zotero headers", h, got)
		}
	}

	// Phase 4: registration.
	reg := file[1]
	if got := reg.Header.Get("If-None-Match"); got != "*" {
		t.Errorf("phase 4 If-None-Match = %q, want *", got)
	}
	regForm, err := url.ParseQuery(reg.Body)
	if err != nil || regForm.Get("upload") != f.uploadKey {
		t.Errorf("phase 4 body = %q, want upload=%s", reg.Body, f.uploadKey)
	}
}

func TestUploadAttachmentFile_DedupSkipsS3AndRegister(t *testing.T) {
	t.Parallel()
	s3, s3URL := newFakeS3(t)
	c, f := newUploadClient(t, s3URL)
	f.exists = true

	uploaded, err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("bytes"), "paper.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if uploaded {
		t.Error("uploaded = true, want false on the dedup short-circuit")
	}
	if n := len(f.fileRequests()); n != 1 {
		t.Errorf("saw %d /file requests, want just the authorization", n)
	}
	if n := len(s3.requests()); n != 0 {
		t.Errorf("S3 saw %d requests, want none", n)
	}
}

func TestUploadAttachmentFile_S3FailureSurfacesBody(t *testing.T) {
	t.Parallel()
	s3, s3URL := newFakeS3(t)
	s3.code = http.StatusBadRequest
	s3.status = "<Error><Code>InvalidArgument</Code><Message>Bucket POST must contain a field named 'key'</Message></Error>"
	c, f := newUploadClient(t, s3URL)

	_, err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("bytes"), "paper.pdf", "application/pdf")
	if err == nil {
		t.Fatal("err = nil, want the S3 failure")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "InvalidArgument") {
		t.Errorf("err = %v, want the S3 status and body", err)
	}
	if n := len(f.fileRequests()); n != 1 {
		t.Errorf("saw %d /file requests, want no registration after a failed S3 post", n)
	}
}

func TestUploadAttachmentFile_Phase2PreconditionFailed(t *testing.T) {
	t.Parallel()
	s3, s3URL := newFakeS3(t)
	c, f := newUploadClient(t, s3URL)
	f.phase2Status = http.StatusPreconditionFailed

	_, err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("bytes"), "paper.pdf", "application/pdf")
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok || herr.Status != http.StatusPreconditionFailed {
		t.Fatalf("err = %v, want an HTTPError with status 412", err)
	}
	if n := len(s3.requests()); n != 0 {
		t.Errorf("S3 saw %d requests, want none after a refused authorization", n)
	}
}

func TestHasAttachmentWithMD5(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("PARENT01", 1, map[string]any{"itemType": "journalArticle"})
	f.seedItem("CHILD001", 2, map[string]any{"itemType": "attachment", "parentItem": "PARENT01", "md5": "0123abcd"})
	f.seedItem("CHILD002", 3, map[string]any{"itemType": "note", "parentItem": "PARENT01"})
	c, _ := newTestClient(t, f)

	got, err := c.HasAttachmentWithMD5(context.Background(), "PARENT01", "0123ABCD")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("HasAttachmentWithMD5 = false, want true (md5 compares case-insensitively)")
	}
	got, err = c.HasAttachmentWithMD5(context.Background(), "PARENT01", "ffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("HasAttachmentWithMD5 = true, want false for an unknown digest")
	}
}
