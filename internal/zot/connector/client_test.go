package connector

// Transport-layer tests for the Zotero desktop connector HTTP surface. These
// cover the three endpoints we hit in production:
//
//   - GET  /connector/ping
//   - POST /connector/saveStandaloneAttachment
//   - POST /connector/getRecognizedItem
//
// Responses with a short `// inferred from …` comment are placeholders we
// wrote from the client source (recognizeDocument.js, saveSession.js); the
// first live curl against desktop may refine them. TDD gives us fast feedback
// when the wire format isn't quite what we expected.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Ping ---

func TestPing_hitsConnectorPingWithNonBrowserUA(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath, gotUA string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/connector/ping" {
		t.Errorf("path = %q, want /connector/ping", gotPath)
	}
	// server.js's browser guard marks any UA starting with "Mozilla/" as a
	// browser and then demands extra headers. Our UA must not trip it.
	if strings.HasPrefix(gotUA, "Mozilla/") {
		t.Errorf("UA = %q, must not start with Mozilla/ — would trip desktop's browser guard", gotUA)
	}
}

func TestPing_refusedBecomesTypedError(t *testing.T) {
	t.Parallel()
	// Point at a closed server address so the dial fails.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // immediately shutter so Dial fails
	c := NewClient(WithBaseURL(srv.URL))

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error when desktop is unreachable")
	}
	if !errors.Is(err, ErrDesktopUnreachable) {
		t.Errorf("err = %v, want wrapping ErrDesktopUnreachable", err)
	}
}

// --- SaveStandaloneAttachment ---

func TestSaveStandaloneAttachment_putsBytesInBodyAsRaw(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	var gotCT, gotAPIVer string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotCT = r.Header.Get("Content-Type")
		gotAPIVer = r.Header.Get("X-Zotero-Connector-API-Version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"canRecognize": true}`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	pdfBytes := []byte("%PDF-1.4 body bytes")
	_, err := c.SaveStandaloneAttachment(context.Background(), strings.NewReader(string(pdfBytes)), SaveMeta{
		SessionID: "SESS-XYZ", URL: "file:///x.pdf", Title: "x.pdf",
	})
	if err != nil {
		t.Fatalf("SaveStandaloneAttachment: %v", err)
	}
	if string(gotBody) != string(pdfBytes) {
		t.Errorf("body mismatch: got %q, want %q", gotBody, pdfBytes)
	}
	if gotCT != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", gotCT)
	}
	if gotAPIVer != "3" {
		t.Errorf("X-Zotero-Connector-API-Version = %q, want 3", gotAPIVer)
	}
}

func TestSaveStandaloneAttachment_setsContentLength(t *testing.T) {
	t.Parallel()
	// Zotero desktop's saveStandaloneAttachment handler demands Content-Length
	// and rejects chunked transfer encoding with 400 "Content-length not
	// provided". A *os.File body can't be length-inferred by net/http, so the
	// client must buffer and set ContentLength itself. Regression guard.
	var gotLen string
	var gotTE []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.Header.Get("Content-Length")
		gotTE = r.TransferEncoding
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"canRecognize": true}`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	pdf := "%PDF-1.4\n" + strings.Repeat("x", 4096)

	// Pass a reader type that doesn't carry length info — i.e. NOT a
	// *bytes.Reader or *strings.Reader. This forces the client's buffering
	// path, which is what *os.File hits in production.
	body := io.Reader(noLenReader{inner: strings.NewReader(pdf)})
	_, err := c.SaveStandaloneAttachment(context.Background(), body, SaveMeta{SessionID: "S"})
	if err != nil {
		t.Fatalf("SaveStandaloneAttachment: %v", err)
	}
	if gotLen == "" {
		t.Errorf("Content-Length header was missing; desktop's handler will 400 this")
	}
	if len(gotTE) > 0 {
		t.Errorf("request must not use Transfer-Encoding (got %v)", gotTE)
	}
}

// noLenReader hides length-inference affordances so Go's http package can't
// auto-set ContentLength from a type assertion.
type noLenReader struct{ inner io.Reader }

func (r noLenReader) Read(p []byte) (int, error) { return r.inner.Read(p) }

func TestSaveStandaloneAttachment_setsXMetadataJSON(t *testing.T) {
	t.Parallel()
	var gotMetaHeader string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMetaHeader = r.Header.Get("X-Metadata")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"canRecognize": true}`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.SaveStandaloneAttachment(context.Background(), strings.NewReader("%PDF"), SaveMeta{
		SessionID: "SESS-ABC", URL: "file:///some/paper.pdf", Title: "paper.pdf",
	})
	if err != nil {
		t.Fatalf("SaveStandaloneAttachment: %v", err)
	}
	// Header must be JSON the server can unmarshal — check the three fields we care about round-trip.
	var meta map[string]any
	if err := json.Unmarshal([]byte(gotMetaHeader), &meta); err != nil {
		t.Fatalf("X-Metadata not valid JSON: %v, raw=%q", err, gotMetaHeader)
	}
	if meta["sessionID"] != "SESS-ABC" || meta["url"] != "file:///some/paper.pdf" || meta["title"] != "paper.pdf" {
		t.Errorf("X-Metadata shape wrong: %+v", meta)
	}
}

func TestSaveStandaloneAttachment_parsesCanRecognize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"recognizable", `{"canRecognize": true}`, true},
		{"notRecognizable", `{"canRecognize": false}`, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(tc.payload))
			})
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)

			c := NewClient(WithBaseURL(srv.URL))
			resp, err := c.SaveStandaloneAttachment(context.Background(), strings.NewReader("x"), SaveMeta{SessionID: "S"})
			if err != nil {
				t.Fatalf("SaveStandaloneAttachment: %v", err)
			}
			if resp.CanRecognize != tc.want {
				t.Errorf("CanRecognize = %v, want %v", resp.CanRecognize, tc.want)
			}
		})
	}
}

func TestSaveStandaloneAttachment_nonOKStatusErrors(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.SaveStandaloneAttachment(context.Background(), strings.NewReader("x"), SaveMeta{SessionID: "S"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
}

// --- GetRecognizedItem ---

func TestGetRecognizedItem_sendsSessionID(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	var gotBody map[string]any
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	_, _ = c.GetRecognizedItem(context.Background(), "SESS-POLL")
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/connector/getRecognizedItem" {
		t.Errorf("path = %q, want /connector/getRecognizedItem", gotPath)
	}
	if gotBody["sessionID"] != "SESS-POLL" {
		t.Errorf("body sessionID = %v", gotBody["sessionID"])
	}
}

func TestGetRecognizedItem_204MeansNoMatch(t *testing.T) {
	t.Parallel()
	// Live-confirmed against desktop (server_connector.js:GetRecognizedItem):
	// when recognition finishes without producing a parent item the handler
	// returns 204 No Content. This is distinct from a transport error — the
	// caller surfaces it as "recognition ran but couldn't identify the PDF".
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	resp, err := c.GetRecognizedItem(context.Background(), "SESS")
	if err != nil {
		t.Fatalf("GetRecognizedItem: %v", err)
	}
	if resp.Recognized {
		t.Error("204 must translate to Recognized=false")
	}
}

func TestGetRecognizedItem_parsesFinishedPayload(t *testing.T) {
	t.Parallel()
	// Live-confirmed against desktop: the handler returns only
	// {title, itemType} — no parent itemKey, no creators. The rest of the
	// bib record lives in the user's library; callers who want the item key
	// have to reconcile via a separate lookup (out of scope for v1).
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Attention Is All You Need",
			"itemType": "journalArticle"
		}`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	resp, err := c.GetRecognizedItem(context.Background(), "SESS")
	if err != nil {
		t.Fatalf("GetRecognizedItem: %v", err)
	}
	if !resp.Recognized {
		t.Errorf("resp.Recognized should be true for populated payload, got %+v", resp)
	}
	if resp.Title != "Attention Is All You Need" {
		t.Errorf("Title = %q", resp.Title)
	}
	if resp.ItemType != "journalArticle" {
		t.Errorf("ItemType = %q, want journalArticle", resp.ItemType)
	}
}
