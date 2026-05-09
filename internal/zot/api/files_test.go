package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// uploadPath is the phase-2/phase-4 endpoint. Keeping it here so every test
// that asserts routing sees the same string and typos fail at one site.
const uploadPath = "/users/42/items/ATTACH01/file"

// readBodyForm parses the request body as application/x-www-form-urlencoded.
// Both phase 2 and phase 4 use this encoding.
func readBodyForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	v, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse form body %q: %v", string(body), err)
	}
	return v
}

func TestRequestUploadAuth_ReturnsAuthorizationWhenFileIsNew(t *testing.T) {
	t.Parallel()

	var gotForm url.Values
	var gotIfNoneMatch string
	var gotContentType string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != uploadPath {
			t.Errorf("unexpected req: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotContentType = r.Header.Get("Content-Type")
		gotForm = readBodyForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"url": "https://s3.example.com/upload",
			"contentType": "application/pdf",
			"prefix": "PRE",
			"suffix": "SUF",
			"uploadKey": "UPLD-KEY-1",
			"params": {"key": "abc", "AWSAccessKeyId": "AKIA"}
		}`))
	})
	c, _ := newTestClient(t, h)

	auth, err := c.requestUploadAuth(context.Background(), "ATTACH01", fileDigest{
		MD5: "5d41402abc4b2a76b9719d911017c592", Size: 5, MTimeMillis: 1700000000000,
	}, "paper.pdf")
	if err != nil {
		t.Fatalf("requestUploadAuth: %v", err)
	}

	if gotIfNoneMatch != "*" {
		t.Errorf(`If-None-Match = %q, want "*"`, gotIfNoneMatch)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Errorf(`Content-Type = %q, want application/x-www-form-urlencoded`, gotContentType)
	}
	if gotForm.Get("md5") != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("md5 form field = %q", gotForm.Get("md5"))
	}
	if gotForm.Get("filename") != "paper.pdf" {
		t.Errorf("filename form field = %q", gotForm.Get("filename"))
	}
	if gotForm.Get("filesize") != "5" {
		t.Errorf("filesize form field = %q, want 5", gotForm.Get("filesize"))
	}
	if gotForm.Get("mtime") != "1700000000000" {
		t.Errorf("mtime form field = %q, want 1700000000000", gotForm.Get("mtime"))
	}

	if auth == nil {
		t.Fatal("auth is nil")
	}
	if auth.URL != "https://s3.example.com/upload" {
		t.Errorf("auth.URL = %q", auth.URL)
	}
	if auth.UploadKey != "UPLD-KEY-1" {
		t.Errorf("auth.UploadKey = %q", auth.UploadKey)
	}
	if auth.Prefix != "PRE" || auth.Suffix != "SUF" {
		t.Errorf("auth prefix/suffix = %q/%q", auth.Prefix, auth.Suffix)
	}
	if auth.ContentType != "application/pdf" {
		t.Errorf("auth.ContentType = %q", auth.ContentType)
	}
	if auth.Params["key"] != "abc" || auth.Params["AWSAccessKeyId"] != "AKIA" {
		t.Errorf("auth.Params = %+v", auth.Params)
	}
}

// --- CreateChildAttachment (phase 1) ----------------------------------------

func TestCreateChildAttachment_PostsImportedFileShape(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/users/42/items" {
			t.Errorf("unexpected req: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"failed": {}, "unchanged": {},
			"successful": {"0": {"key": "NEWATT01", "version": 1}}
		}`))
	})
	c, _ := newTestClient(t, h)

	it, err := c.CreateChildAttachment(context.Background(), "PARENT00", AttachmentMeta{
		Filename: "paper.pdf", ContentType: "application/pdf", Title: "Hello PDF",
	})
	if err != nil {
		t.Fatalf("CreateChildAttachment: %v", err)
	}
	if it == nil || it.Key != "NEWATT01" {
		t.Errorf("it = %+v, want Key=NEWATT01", it)
	}

	// The body MUST be a one-element array with the required attachment
	// fields set. We check shape, not exact bytes, so non-meaningful key
	// ordering doesn't make the test flaky.
	var batch []map[string]any
	if err := json.Unmarshal(gotBody, &batch); err != nil {
		t.Fatalf("decode body: %v — body=%s", err, gotBody)
	}
	if len(batch) != 1 {
		t.Fatalf("batch length = %d, want 1", len(batch))
	}
	data := batch[0]
	if data["itemType"] != "attachment" {
		t.Errorf("itemType = %v, want attachment", data["itemType"])
	}
	if data["linkMode"] != "imported_file" {
		t.Errorf("linkMode = %v, want imported_file", data["linkMode"])
	}
	if data["parentItem"] != "PARENT00" {
		t.Errorf("parentItem = %v, want PARENT00", data["parentItem"])
	}
	if data["filename"] != "paper.pdf" {
		t.Errorf("filename = %v, want paper.pdf", data["filename"])
	}
	if data["contentType"] != "application/pdf" {
		t.Errorf("contentType = %v, want application/pdf", data["contentType"])
	}
	if data["title"] != "Hello PDF" {
		t.Errorf("title = %v, want Hello PDF", data["title"])
	}
}

// --- UploadAttachmentFile orchestrator ---------------------------------------

// zoteroAndS3 wires a fake Zotero endpoint and a fake S3 endpoint into one
// test harness. Useful only to the orchestrator tests below — the per-phase
// tests use their own single-server doubles.
type zoteroAndS3 struct {
	zoteroCalls []string              // method+path appended on every hit
	s3Calls     int                   // multipart POSTs received
	phase2JSON  string                // what the Zotero handler returns for phase 2
	s3Status    int                   // status for the S3 POST (default 201)
	onPhase2    func(form url.Values) // optional assertion hook
	onPhase4    func(form url.Values) // optional assertion hook
	onS3        func(body []byte, ct string)
}

func (z *zoteroAndS3) zoteroHandler(t *testing.T, s3URL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z.zoteroCalls = append(z.zoteroCalls, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodPost || r.URL.Path != uploadPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		form := readBodyForm(t, r)
		// Phase 4 requests carry an `upload` field; phase 2 carries md5 etc.
		if form.Get("upload") != "" {
			if z.onPhase4 != nil {
				z.onPhase4(form)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if z.onPhase2 != nil {
			z.onPhase2(form)
		}
		payload := z.phase2JSON
		if payload == "" {
			payload = `{"url":"` + s3URL + `","contentType":"application/pdf","prefix":"P","suffix":"S","uploadKey":"UPLD","params":{"key":"k"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	})
}

func (z *zoteroAndS3) s3Server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z.s3Calls++
		if z.onS3 != nil {
			b, _ := io.ReadAll(r.Body)
			z.onS3(b, r.Header.Get("Content-Type"))
		}
		status := z.s3Status
		if status == 0 {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	}))
}

func TestUploadAttachmentFile_HappyPathHitsAllThreePhasesInOrder(t *testing.T) {
	t.Parallel()

	z := &zoteroAndS3{}
	var phase2Seen, s3Seen, phase4Seen bool
	z.onPhase2 = func(form url.Values) {
		phase2Seen = true
		if !s3Seen && !phase4Seen {
			return // good ordering
		}
		t.Error("phase 2 observed after phase 3/4")
	}
	z.onS3 = func(_ []byte, _ string) {
		s3Seen = true
		if !phase2Seen {
			t.Error("phase 3 fired before phase 2")
		}
	}
	z.onPhase4 = func(form url.Values) {
		phase4Seen = true
		if !phase2Seen || !s3Seen {
			t.Error("phase 4 fired before prerequisites")
		}
		if form.Get("upload") != "UPLD" {
			t.Errorf("phase 4 upload key = %q, want UPLD", form.Get("upload"))
		}
	}

	s3 := z.s3Server(t)
	t.Cleanup(s3.Close)
	z.phase2JSON = `{"url":"` + s3.URL + `","contentType":"application/pdf","prefix":"","suffix":"","uploadKey":"UPLD","params":{"key":"k"}}`

	c, _ := newTestClient(t, z.zoteroHandler(t, s3.URL))
	if err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("%PDF-1.4\n"), "paper.pdf", "application/pdf"); err != nil {
		t.Fatalf("UploadAttachmentFile: %v", err)
	}
	if !phase2Seen || !s3Seen || !phase4Seen {
		t.Errorf("phases observed: p2=%v p3=%v p4=%v", phase2Seen, s3Seen, phase4Seen)
	}
	if z.s3Calls != 1 {
		t.Errorf("s3 calls = %d, want 1", z.s3Calls)
	}
}

func TestUploadAttachmentFile_ExistsShortCircuitsPhase3And4(t *testing.T) {
	t.Parallel()
	z := &zoteroAndS3{phase2JSON: `{"exists":1}`}
	s3 := z.s3Server(t)
	t.Cleanup(s3.Close)

	c, _ := newTestClient(t, z.zoteroHandler(t, s3.URL))
	if err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("x"), "dup.pdf", "application/pdf"); err != nil {
		t.Fatalf("UploadAttachmentFile: %v", err)
	}
	if z.s3Calls != 0 {
		t.Errorf("S3 must not be contacted when dedup fires, got %d calls", z.s3Calls)
	}
	if got := len(z.zoteroCalls); got != 1 {
		t.Errorf("zotero calls = %d, want 1 (phase 2 only)", got)
	}
}

func TestUploadAttachmentFile_S3FailureStopsBeforePhase4(t *testing.T) {
	t.Parallel()
	z := &zoteroAndS3{s3Status: http.StatusForbidden}
	s3 := z.s3Server(t)
	t.Cleanup(s3.Close)
	z.phase2JSON = `{"url":"` + s3.URL + `","contentType":"application/pdf","prefix":"","suffix":"","uploadKey":"UPLD","params":{}}`

	c, _ := newTestClient(t, z.zoteroHandler(t, s3.URL))
	err := c.UploadAttachmentFile(context.Background(), "ATTACH01", strings.NewReader("x"), "p.pdf", "application/pdf")
	if err == nil {
		t.Fatal("want error when phase 3 fails")
	}
	// Exactly one Zotero call (phase 2). Phase 4 must not have fired.
	if got := len(z.zoteroCalls); got != 1 {
		t.Errorf("zotero calls = %d, want 1 (phase 2 only); got %v", got, z.zoteroCalls)
	}
}

func TestRegisterUpload_PostsUploadKeyAndExpects204(t *testing.T) {
	t.Parallel()
	var (
		gotForm        url.Values
		gotIfNoneMatch string
		gotContentType string
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != uploadPath {
			t.Errorf("unexpected req: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotContentType = r.Header.Get("Content-Type")
		gotForm = readBodyForm(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestClient(t, h)

	if err := c.registerUpload(context.Background(), "ATTACH01", "UPLD-KEY-1"); err != nil {
		t.Fatalf("registerUpload: %v", err)
	}
	if gotIfNoneMatch != "*" {
		t.Errorf(`If-None-Match = %q, want "*"`, gotIfNoneMatch)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Errorf(`Content-Type = %q, want application/x-www-form-urlencoded`, gotContentType)
	}
	if gotForm.Get("upload") != "UPLD-KEY-1" {
		t.Errorf("upload form field = %q", gotForm.Get("upload"))
	}
	// Phase-4 body must be JUST `upload=<key>`; md5 etc. belong to phase 2
	// and would confuse Zotero if echoed here.
	if len(gotForm) != 1 {
		t.Errorf("unexpected extra form fields: %v", gotForm)
	}
}

func TestRegisterUpload_NonSuccessStatusIsError(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	})
	c, _ := newTestClient(t, h, WithMaxRetries(1))

	err := c.registerUpload(context.Background(), "ATTACH01", "UPLD-KEY-1")
	if err == nil {
		t.Fatal("want error on 500")
	}
}

// TestUploadToS3_SendsCanonicalBody locks in the canonical phase-3 body:
// raw `Prefix + bytes + Suffix` with Content-Type echoed verbatim from the
// auth response. The previous implementation built its own multipart from
// auth.Params and wrapped Prefix+bytes+Suffix inside a "file" form field;
// for upload-auth responses where Params was empty (Zotero bakes the form
// fields entirely into Prefix instead), that produced a body with `key=""`
// and S3 rejected with `400 InvalidArgument: Bucket POST must contain a
// field named 'key'`. ~4% of real uploads went down that path.
func TestUploadToS3_SendsCanonicalBody(t *testing.T) {
	t.Parallel()

	var (
		gotBody []byte
		gotCT   string
		gotLen  int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotLen = r.ContentLength
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated) // S3 answers 201 on POST success
	}))
	t.Cleanup(srv.Close)

	auth := &UploadAuthorization{
		URL:         srv.URL,
		ContentType: "multipart/form-data; boundary=----abc",
		Prefix:      "--PRE--",
		Suffix:      "--SUF--",
		UploadKey:   "UPLD-KEY-1",
	}
	fileBytes := []byte("%PDF-1.4\nhello")

	if err := uploadToS3(context.Background(), http.DefaultClient, auth, fileBytes); err != nil {
		t.Fatalf("uploadToS3: %v", err)
	}

	wantBody := slices.Concat([]byte(auth.Prefix), fileBytes, []byte(auth.Suffix))
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("body bytes mismatch:\n got %q\nwant %q", gotBody, wantBody)
	}
	if gotCT != auth.ContentType {
		t.Errorf("Content-Type = %q, want %q (verbatim from auth.ContentType)", gotCT, auth.ContentType)
	}
	// Content-Length must be set explicitly so the receiver doesn't see chunked
	// transfer (some endpoints reject that, mirroring connector saveStandaloneAttachment).
	if gotLen != int64(len(wantBody)) {
		t.Errorf("Content-Length = %d, want %d", gotLen, len(wantBody))
	}
}

// TestUploadToS3_EmptyParamsStillUploads is the regression for the 4% bug.
// Even with no Params at all, the canonical body path delivers a complete
// upload — Prefix carries the form structure.
func TestUploadToS3_EmptyParamsStillUploads(t *testing.T) {
	t.Parallel()
	var saw bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		saw = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	auth := &UploadAuthorization{
		URL:         srv.URL,
		ContentType: "multipart/form-data; boundary=----xyz",
		Prefix:      "preamble-with-baked-key-policy-signature",
		Suffix:      "epilogue",
		Params:      map[string]string{}, // empty — the legacy/bug case
	}
	if err := uploadToS3(context.Background(), http.DefaultClient, auth, []byte("data")); err != nil {
		t.Fatalf("uploadToS3 must succeed even when Params is empty: %v", err)
	}
	if !saw {
		t.Error("server didn't see the request")
	}
}

func TestUploadToS3_NonSuccessStatusIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "access denied", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	auth := &UploadAuthorization{URL: srv.URL, ContentType: "application/pdf"}
	err := uploadToS3(context.Background(), http.DefaultClient, auth, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want non-nil 403", err)
	}
}

func TestRequestUploadAuth_ReturnsExistsSentinelWhenFilePresent(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exists": 1}`))
	})
	c, _ := newTestClient(t, h)

	auth, err := c.requestUploadAuth(context.Background(), "ATTACH01", fileDigest{
		MD5: "00000000000000000000000000000000", Size: 0, MTimeMillis: 0,
	}, "ghost.pdf")
	if !errors.Is(err, errUploadExists) {
		t.Errorf("err = %v, want errUploadExists", err)
	}
	if auth != nil {
		t.Errorf("auth must be nil on exists response, got %+v", auth)
	}
}
