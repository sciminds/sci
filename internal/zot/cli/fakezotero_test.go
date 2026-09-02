package cli

// A small in-memory Zotero Web API for the write-verb tests: single-object
// GET/PATCH/DELETE and multi-object POST for items and collections under one
// library prefix, with Zotero's optimistic concurrency (a stale
// If-Unmodified-Since-Version is a 412). internal/zot/api/fake_test.go is the
// fuller reference; this one carries only what the cli verbs exercise.

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
)

// fakeObject is one stored item or collection: version plus the generic
// data payload, the way Zotero returns objects.
type fakeObject struct {
	Version int
	Data    map[string]any
}

type fakeZotero struct {
	t      *testing.T
	prefix string
	mu     sync.Mutex
	items  map[string]*fakeObject
	colls  map[string]*fakeObject
	// forbidden makes every library request answer 403, the way Zotero
	// treats a key without write access to the library.
	forbidden bool
	// conflicts scripts a competing writer: while positive, every versioned
	// write bumps the object's version and answers 412 instead.
	conflicts int
	// failNextWrite makes the next multi-object POST answer with a failed
	// slot carrying this message instead of creating anything.
	failNextWrite string
	nextKey       int
	requests      []string
}

// requestCount is how many requests the server has seen — zero is the
// "never reached" assertion a usage error must satisfy.
func (f *fakeZotero) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeZotero) setConflicts(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conflicts = n
}

// installFakeZotero starts a fake server for the personal library of user
// 42 (what saveWithCredential stores) and points requireAPIClient at it via
// the newAPIClient seam. Sleeps are skipped so a retry never waits. The seam
// is a package global, so callers must not t.Parallel().
func installFakeZotero(t *testing.T) *fakeZotero {
	t.Helper()
	f := &fakeZotero{
		t:      t,
		prefix: "/users/42",
		items:  map[string]*fakeObject{},
		colls:  map[string]*fakeObject{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	prev := newAPIClient
	newAPIClient = func(creds api.Credentials, lib zot.LibraryRef) (*api.Client, error) {
		return api.New(creds, lib, api.WithBaseURL(srv.URL), api.WithSleep(func(time.Duration) {}))
	}
	t.Cleanup(func() { newAPIClient = prev })
	return f
}

func (f *fakeZotero) seedItem(key string, collections ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key] = &fakeObject{Version: 10, Data: map[string]any{
		"itemType":    "journalArticle",
		"collections": lo.ToAnySlice(collections),
	}}
}

func (f *fakeZotero) seedCollection(key, name, parentKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var parent any = false
	if parentKey != "" {
		parent = parentKey
	}
	f.colls[key] = &fakeObject{Version: 20, Data: map[string]any{"name": name, "parentCollection": parent}}
}

func (f *fakeZotero) item(key string) *fakeObject {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items[key]
}

func (f *fakeZotero) collection(key string) *fakeObject {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.colls[key]
}

// itemCollections reads an item's collection membership as strings.
func (f *fakeZotero) itemCollections(key string) []string {
	obj := f.item(key)
	if obj == nil {
		return nil
	}
	raw, _ := obj.Data["collections"].([]any)
	return lo.Map(raw, func(v any, _ int) string { return v.(string) })
}

func (f *fakeZotero) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	if f.forbidden {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, f.prefix+"/items"):
		f.serveObjects(w, r, f.items, strings.TrimPrefix(r.URL.Path, f.prefix+"/items"), body)
	case strings.HasPrefix(r.URL.Path, f.prefix+"/collections"):
		f.serveObjects(w, r, f.colls, strings.TrimPrefix(r.URL.Path, f.prefix+"/collections"), body)
	default:
		http.Error(w, "unexpected route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *fakeZotero) serveObjects(w http.ResponseWriter, r *http.Request, store map[string]*fakeObject, rest string, body []byte) {
	if rest == "" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.serveCreate(w, store, body)
		return
	}
	key := strings.TrimPrefix(rest, "/")
	obj, ok := store[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeFakeJSON(w, http.StatusOK, wireObject(key, obj))
	case http.MethodPatch, http.MethodDelete:
		sent, err := strconv.Atoi(r.Header.Get("If-Unmodified-Since-Version"))
		if err != nil {
			f.t.Errorf("%s %s: missing or non-numeric If-Unmodified-Since-Version", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if f.conflicts > 0 {
			f.conflicts--
			obj.Version++ // someone else wrote in between
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if sent != obj.Version {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Method == http.MethodDelete {
			delete(store, key)
		} else {
			var patch map[string]any
			if err := json.Unmarshal(body, &patch); err != nil {
				http.Error(w, "bad patch: "+err.Error(), http.StatusBadRequest)
				return
			}
			maps.Copy(obj.Data, patch)
			obj.Version++
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeZotero) serveCreate(w http.ResponseWriter, store map[string]*fakeObject, body []byte) {
	var payloads []map[string]any
	if err := json.Unmarshal(body, &payloads); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if f.failNextWrite != "" {
		msg := f.failNextWrite
		f.failNextWrite = ""
		writeFakeJSON(w, http.StatusOK, map[string]any{
			"successful": map[string]any{},
			"unchanged":  map[string]any{},
			"failed":     map[string]any{"0": map[string]any{"code": 400, "message": msg}},
		})
		return
	}
	successful := map[string]any{}
	for i, p := range payloads {
		f.nextKey++
		key := fmt.Sprintf("NEW%05d", f.nextKey)
		obj := &fakeObject{Version: 100 + f.nextKey, Data: p}
		store[key] = obj
		successful[strconv.Itoa(i)] = wireObject(key, obj)
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"successful": successful,
		"unchanged":  map[string]any{},
		"failed":     map[string]any{},
	})
}

// wireObject renders a stored object the way the API does: key and version
// at the top level and again inside data.
func wireObject(key string, obj *fakeObject) map[string]any {
	data := maps.Clone(obj.Data)
	data["key"] = key
	data["version"] = obj.Version
	return map[string]any{"key": key, "version": obj.Version, "data": data}
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
