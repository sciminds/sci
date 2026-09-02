package api

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot"
)

// testCreds is the credential set every test client is built with. UserID 42
// and GroupID 777 are what the fake server's library prefixes are keyed on.
var testCreds = Credentials{APIKey: "test-key", UserID: "42", GroupID: "777"}

// sleepLog records every sleep the client asks for instead of pausing, so a
// test can assert on the retry schedule without waiting through it.
type sleepLog struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func (l *sleepLog) sleep(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sleeps = append(l.sleeps, d)
}

func (l *sleepLog) total() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var sum time.Duration
	for _, d := range l.sleeps {
		sum += d
	}
	return sum
}

// newTestClient starts handler on an in-memory network and returns a client
// pointed at it. The client targets the personal library unless opts say
// otherwise; sleeps are recorded, never taken.
func newTestClient(t *testing.T, handler http.Handler, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	return newTestClientFor(t, handler, zot.LibraryRef{Scope: zot.LibPersonal, Name: "Personal"}, opts...)
}

func newTestClientFor(t *testing.T, handler http.Handler, lib zot.LibraryRef, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	srv := httptest.NewTestServer(t, handler)
	srv.Start()
	log := &sleepLog{}
	all := append([]Option{
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithSleep(log.sleep),
	}, opts...)
	c, err := New(testCreds, lib, all...)
	if err != nil {
		t.Fatal(err)
	}
	return c, log
}

// recordedRequest is one request the fake server saw, kept for assertions.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

// fakeObject is one stored item or collection: the server-side version plus
// the generic data payload, mirroring how Zotero returns objects.
type fakeObject struct {
	Version int
	Data    map[string]any
}

// fakeZotero is a small in-memory Zotero server covering exactly the routes
// the client uses: single-object GET/PATCH/DELETE and multi-object POST for
// items and collections under a library prefix, plus the two root endpoints.
//
// It enforces optimistic concurrency the way Zotero does (a PATCH or DELETE
// whose If-Unmodified-Since-Version is stale gets a 412) and can be scripted
// to fake a competing writer via conflicts: while it is positive, every
// versioned write bumps the object's version and answers 412 instead.
type fakeZotero struct {
	t         *testing.T
	prefix    string // "/users/42" or "/groups/777"
	mu        sync.Mutex
	items     map[string]*fakeObject
	colls     map[string]*fakeObject
	requests  []recordedRequest
	conflicts int
	// onConflict, when set, is the competing writer: it runs against the
	// object each time a scripted conflict fires, so a re-read sees what
	// the other writer changed and not just a bumped version.
	onConflict func(obj *fakeObject)
	nextKey    int
	// failNextWrite, when set, makes the next multi-object POST answer with a
	// failed slot carrying this message instead of creating anything.
	failNextWrite string
	// fields backs GET /itemTypeFields; keyed by item type.
	fields map[string][]string
	// creatorTypes backs GET /itemTypeCreatorTypes; keyed by item type.
	creatorTypes map[string][]string
}

func newFakeZotero(t *testing.T) *fakeZotero {
	return &fakeZotero{
		t:            t,
		prefix:       "/users/42",
		items:        map[string]*fakeObject{},
		colls:        map[string]*fakeObject{},
		fields:       map[string][]string{},
		creatorTypes: map[string][]string{},
	}
}

func (f *fakeZotero) seedItem(key string, version int, data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key] = &fakeObject{Version: version, Data: data}
}

func (f *fakeZotero) seedCollection(key string, version int, data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.colls[key] = &fakeObject{Version: version, Data: data}
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

func (f *fakeZotero) setConflicts(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conflicts = n
}

func (f *fakeZotero) seen() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// methods lists the method+path of every request seen, in order — the shape
// most assertions about "what did the client do" want.
func (f *fakeZotero) methods() []string {
	return lo.Map(f.seen(), func(r recordedRequest, _ int) string { return r.Method + " " + r.Path })
}

func (f *fakeZotero) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	bodyBytes, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Header: r.Header.Clone(),
		Body:   string(bodyBytes),
	})

	switch {
	case r.URL.Path == "/keys/current":
		writeJSON(w, http.StatusOK, map[string]any{"key": "test-key", "userID": 42, "username": "alice"})
	case r.URL.Path == "/itemTypeFields":
		names := f.fields[r.URL.Query().Get("itemType")]
		out := lo.Map(names, func(n string, _ int) map[string]string {
			return map[string]string{"field": n, "localized": strings.ToUpper(n)}
		})
		writeJSON(w, http.StatusOK, out)
	case r.URL.Path == "/itemTypeCreatorTypes":
		names := f.creatorTypes[r.URL.Query().Get("itemType")]
		out := lo.Map(names, func(n string, _ int) map[string]string {
			return map[string]string{"creatorType": n, "localized": strings.ToUpper(n)}
		})
		writeJSON(w, http.StatusOK, out)
	case strings.HasPrefix(r.URL.Path, f.prefix+"/items"):
		f.serveObjects(w, r, f.items, strings.TrimPrefix(r.URL.Path, f.prefix+"/items"), bodyBytes)
	case strings.HasPrefix(r.URL.Path, f.prefix+"/collections"):
		f.serveObjects(w, r, f.colls, strings.TrimPrefix(r.URL.Path, f.prefix+"/collections"), bodyBytes)
	default:
		http.Error(w, "unexpected route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

// serveList answers the collection GET the way Zotero pages it: objects
// with version > since, in key order, sliced by start/limit, with the
// Total-Results header carrying the unsliced count.
func (f *fakeZotero) serveList(w http.ResponseWriter, r *http.Request, store map[string]*fakeObject) {
	q := r.URL.Query()
	since, _ := strconv.Atoi(q.Get("since"))
	start, _ := strconv.Atoi(q.Get("start"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 25
	}
	keys := slices.Sorted(maps.Keys(store))
	matching := lo.Filter(keys, func(k string, _ int) bool { return store[k].Version > since })
	w.Header().Set("Total-Results", strconv.Itoa(len(matching)))
	page := lo.Slice(matching, start, start+limit)
	writeJSON(w, http.StatusOK, lo.Map(page, func(k string, _ int) map[string]any { return wireObject(k, store[k]) }))
}

// serveObjects handles one object family (items or collections). rest is the
// path after the family segment: "" for the collection endpoint, "/KEY" for a
// single object, "/KEY/children" for an item's children.
func (f *fakeZotero) serveObjects(w http.ResponseWriter, r *http.Request, store map[string]*fakeObject, rest string, body []byte) {
	if rest == "" {
		switch r.Method {
		case http.MethodPost:
			f.serveCreate(w, store, body)
		case http.MethodGet:
			f.serveList(w, r, store)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	key, tail, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	if tail == "children" {
		var children []map[string]any
		for k, obj := range store {
			if obj.Data["parentItem"] == key {
				children = append(children, wireObject(k, obj))
			}
		}
		writeJSON(w, http.StatusOK, children)
		return
	}
	obj, ok := store[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, wireObject(key, obj))
	case http.MethodPatch, http.MethodDelete:
		if !f.versionMatches(r, obj) {
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

// versionMatches applies the optimistic-concurrency rule and the scripted
// competing writer. Must be called with f.mu held.
func (f *fakeZotero) versionMatches(r *http.Request, obj *fakeObject) bool {
	sent, err := strconv.Atoi(r.Header.Get("If-Unmodified-Since-Version"))
	if err != nil {
		f.t.Errorf("%s %s: missing or non-numeric If-Unmodified-Since-Version", r.Method, r.URL.Path)
		return false
	}
	if f.conflicts > 0 {
		f.conflicts--
		obj.Version++ // someone else wrote in between
		if f.onConflict != nil {
			f.onConflict(obj)
		}
		return false
	}
	return sent == obj.Version
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
		writeJSON(w, http.StatusOK, map[string]any{
			"successful": map[string]any{},
			"success":    map[string]any{},
			"unchanged":  map[string]any{},
			"failed":     map[string]any{"0": map[string]any{"code": 400, "message": msg}},
		})
		return
	}
	successful := map[string]any{}
	success := map[string]any{}
	for i, p := range payloads {
		f.nextKey++
		key := fmt.Sprintf("NEW%05d", f.nextKey)
		obj := &fakeObject{Version: 100 + f.nextKey, Data: p}
		store[key] = obj
		idx := strconv.Itoa(i)
		successful[idx] = wireObject(key, obj)
		success[idx] = key
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"successful": successful,
		"success":    success,
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// keyInfoJSON is the /keys/current body handlers reuse when a test only
// needs some 2xx response with a parseable payload.
const keyInfoJSON = `{"key":"test-key","userID":42,"username":"alice"}`
