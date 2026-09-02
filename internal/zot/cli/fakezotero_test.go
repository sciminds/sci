package cli

// A small in-memory Zotero Web API for the write-verb tests: single-object
// GET/PATCH/DELETE and multi-object POST for items and collections under one
// library prefix, an item's /children, the two schema endpoints, and the
// /file upload phases with a loopback S3 — all with Zotero's optimistic
// concurrency (a stale If-Unmodified-Since-Version is a 412).
// internal/zot/api/fake_test.go is the fuller reference; this one carries
// only what the cli verbs exercise.

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
	// failNextUpload makes the next upload authorization (phase 2) answer
	// 400 with this message, so a caller sees a failed attach.
	failNextUpload string
	// uploadExists makes phase 2 answer Zotero's dedup short-circuit
	// ({"exists":1}) instead of an authorization.
	uploadExists bool
	// failS3 makes the loopback S3 answer 500 to phase 3.
	failS3 bool
	// s3URL is where phase 2 sends the bytes: this same server's /s3 route.
	s3URL string
	// s3Bodies are the raw phase-3 bodies received, in order.
	s3Bodies []string
	// fields and creatorTypes back the two schema endpoints, keyed by item
	// type; an item type in neither is answered 400 like Zotero does.
	fields       map[string][]string
	creatorTypes map[string][]string
	nextKey      int
	requests     []string
}

// fakeUploadKey is the phase-2 upload key phase 4 must echo back.
const fakeUploadKey = "UPLOADKEY001"

// defaultFakeFields is the schema the fake ships with — enough of Zotero's
// real field lists for the item types item add / update produce.
var defaultFakeFields = map[string][]string{
	"journalArticle":  {"title", "abstractNote", "publicationTitle", "volume", "issue", "pages", "date", "series", "seriesTitle", "journalAbbreviation", "language", "DOI", "ISSN", "shortTitle", "url", "accessDate", "extra"},
	"preprint":        {"title", "abstractNote", "genre", "repository", "archiveID", "place", "date", "series", "seriesNumber", "DOI", "url", "accessDate", "language", "extra"},
	"book":            {"title", "abstractNote", "series", "seriesNumber", "volume", "edition", "place", "publisher", "date", "numPages", "language", "ISBN", "shortTitle", "url", "extra"},
	"conferencePaper": {"title", "abstractNote", "date", "proceedingsTitle", "conferenceName", "place", "publisher", "volume", "pages", "series", "language", "DOI", "ISBN", "url", "extra"},
	"document":        {"title", "abstractNote", "publisher", "date", "language", "url", "extra"},
}

var defaultFakeCreatorTypes = map[string][]string{
	"journalArticle":  {"author", "contributor", "editor", "translator", "reviewedAuthor"},
	"preprint":        {"author", "contributor", "editor", "translator", "reviewedAuthor"},
	"book":            {"author", "contributor", "editor", "translator", "seriesEditor"},
	"conferencePaper": {"author", "contributor", "editor", "translator", "seriesEditor"},
	"document":        {"author", "contributor", "editor", "translator", "reviewedAuthor"},
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

// requestsMatching returns the recorded "METHOD /path" lines containing
// substr — "POST " to count writes, "/file" to count upload phases.
func (f *fakeZotero) requestsMatching(substr string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return lo.Filter(f.requests, func(r string, _ int) bool { return strings.Contains(r, substr) })
}

// uploadedBodies returns what the loopback S3 received, in order.
func (f *fakeZotero) uploadedBodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.s3Bodies)
}

// installFakeZotero starts a fake server for the personal library of user
// 42 (what saveWithCredential stores) and points requireAPIClient at it via
// the newAPIClient seam. Sleeps are skipped so a retry never waits. The seam
// is a package global, so callers must not t.Parallel().
func installFakeZotero(t *testing.T) *fakeZotero {
	t.Helper()
	f := &fakeZotero{
		t:            t,
		prefix:       "/users/42",
		items:        map[string]*fakeObject{},
		colls:        map[string]*fakeObject{},
		fields:       maps.Clone(defaultFakeFields),
		creatorTypes: maps.Clone(defaultFakeCreatorTypes),
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.s3URL = srv.URL + "/s3"
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

// seedItemData seeds an item with an arbitrary data payload (itemType
// included), for the verbs that read fields back.
func (f *fakeZotero) seedItemData(key string, data map[string]any) {
	f.seedItemVersioned(key, 10, data)
}

// seedItemVersioned is seedItemData with an explicit version — what the
// dedup gate's since= query filters on.
func (f *fakeZotero) seedItemVersioned(key string, version int, data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key] = &fakeObject{Version: version, Data: data}
}

// seedAttachment seeds a child attachment carrying an md5, the shape
// HasAttachmentWithMD5 looks for.
func (f *fakeZotero) seedAttachment(key, parentKey, md5sum string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key] = &fakeObject{Version: 10, Data: map[string]any{
		"itemType": "attachment", "linkMode": "imported_file", "parentItem": parentKey, "md5": md5sum,
	}}
}

// children returns the keys of every item whose parentItem is key, sorted.
func (f *fakeZotero) children(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := lo.FilterMap(lo.Entries(f.items), func(e lo.Entry[string, *fakeObject], _ int) (string, bool) {
		return e.Key, e.Value.Data["parentItem"] == key
	})
	slices.Sort(keys)
	return keys
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
	line := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		line += "?" + r.URL.RawQuery
	}
	f.requests = append(f.requests, line)

	if f.forbidden {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/itemTypeFields":
		f.serveSchema(w, r, f.fields, "field")
	case r.URL.Path == "/itemTypeCreatorTypes":
		f.serveSchema(w, r, f.creatorTypes, "creatorType")
	case r.URL.Path == "/s3":
		// Phase 3: the pre-signed POST. No Zotero headers may reach it.
		if r.Header.Get("Zotero-API-Key") != "" {
			http.Error(w, "signature mismatch: unexpected header", http.StatusForbidden)
			return
		}
		if f.failS3 {
			http.Error(w, "<Error><Code>InternalError</Code></Error>", http.StatusInternalServerError)
			return
		}
		f.s3Bodies = append(f.s3Bodies, string(body))
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/file") && strings.HasPrefix(r.URL.Path, f.prefix+"/items/"):
		key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, f.prefix+"/items/"), "/file")
		f.serveFile(w, key, body)
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
		switch r.Method {
		case http.MethodPost:
			f.serveCreate(w, store, body)
		case http.MethodGet:
			// Zotero's paged listing: version > since, key order, start/limit.
			q := r.URL.Query()
			since, _ := strconv.Atoi(q.Get("since"))
			start, _ := strconv.Atoi(q.Get("start"))
			limit, _ := strconv.Atoi(q.Get("limit"))
			if limit <= 0 {
				limit = 25
			}
			keys := lo.Filter(slices.Sorted(maps.Keys(store)), func(k string, _ int) bool { return store[k].Version > since })
			w.Header().Set("Total-Results", strconv.Itoa(len(keys)))
			page := lo.Slice(keys, start, start+limit)
			writeFakeJSON(w, http.StatusOK, lo.Map(page, func(k string, _ int) map[string]any { return wireObject(k, store[k]) }))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	key, tail, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	if tail == "children" {
		children := lo.FilterMap(lo.Entries(store), func(e lo.Entry[string, *fakeObject], _ int) (map[string]any, bool) {
			return wireObject(e.Key, e.Value), e.Value.Data["parentItem"] == key
		})
		writeFakeJSON(w, http.StatusOK, children)
		return
	}
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

// serveSchema answers one of the two schema endpoints from its map; an
// unknown item type is a 400, as on the real API.
func (f *fakeZotero) serveSchema(w http.ResponseWriter, r *http.Request, table map[string][]string, nameKey string) {
	names, ok := table[r.URL.Query().Get("itemType")]
	if !ok {
		http.Error(w, "Invalid item type", http.StatusBadRequest)
		return
	}
	writeFakeJSON(w, http.StatusOK, lo.Map(names, func(n string, _ int) map[string]string {
		return map[string]string{nameKey: n, "localized": strings.ToUpper(n)}
	}))
}

// serveFile is the per-attachment /file endpoint: phase 2 (authorize, or
// the dedup short-circuit) and phase 4 (register, which stamps the md5 the
// authorization carried onto the attachment so later dedup checks see it).
func (f *fakeZotero) serveFile(w http.ResponseWriter, key string, body []byte) {
	obj, ok := f.items[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	if form.Has("upload") {
		if form.Get("upload") != fakeUploadKey {
			http.Error(w, "unknown upload key", http.StatusBadRequest)
			return
		}
		obj.Data["md5"] = obj.Data["pendingMD5"]
		delete(obj.Data, "pendingMD5")
		obj.Version++
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case f.failNextUpload != "":
		msg := f.failNextUpload
		f.failNextUpload = ""
		http.Error(w, msg, http.StatusBadRequest)
	case f.uploadExists:
		obj.Data["md5"] = form.Get("md5")
		writeFakeJSON(w, http.StatusOK, map[string]any{"exists": 1})
	default:
		obj.Data["pendingMD5"] = form.Get("md5")
		writeFakeJSON(w, http.StatusOK, map[string]any{
			"url":         f.s3URL,
			"contentType": "multipart/form-data; boundary=abc123",
			"prefix":      "--abc123\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\n",
			"suffix":      "\r\n--abc123--\r\n",
			"uploadKey":   fakeUploadKey,
			"params":      map[string]any{},
		})
	}
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
