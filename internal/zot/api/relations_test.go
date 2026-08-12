package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"sync"
	"testing"

	"github.com/sciminds/sci/internal/zot/client"
)

func TestItemURI(t *testing.T) {
	tests := []struct {
		apiPath string
		key     string
		want    string
	}{
		{"users/17450224", "B2TX5SEV", "http://zotero.org/users/17450224/items/B2TX5SEV"},
		{"groups/6506098", "K2G5W7TN", "http://zotero.org/groups/6506098/items/K2G5W7TN"},
	}
	for _, tt := range tests {
		if got := itemURI(tt.apiPath, tt.key); got != tt.want {
			t.Errorf("itemURI(%q, %q) = %q, want %q", tt.apiPath, tt.key, got, tt.want)
		}
	}
}

// http, not https: that is the form Zotero itself writes, and the URI is an
// opaque identifier rather than a fetchable address. Normalizing it to https
// would make our relations not match the ones already in the library.
func TestItemURI_UsesHTTPNotHTTPS(t *testing.T) {
	if got := itemURI("users/1", "AAAA1111"); got[:5] != "http:" {
		t.Errorf("got %q, want an http:// URI", got)
	}
}

func TestKeyFromURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"http://zotero.org/users/17450224/items/B2TX5SEV", "B2TX5SEV"},
		{"http://zotero.org/groups/6506098/items/K2G5W7TN", "K2G5W7TN"},
		{"not a uri", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := keyFromURI(tt.uri); got != tt.want {
			t.Errorf("keyFromURI(%q) = %q, want %q", tt.uri, tt.want, tt.want)
		}
	}
}

// Zotero's schema types each predicate's value as string-or-array. Both
// forms appear in real libraries, so decoding has to accept either — a
// single relation is often written as a bare string.
func TestDecodeRelations_AcceptsBothScalarAndArray(t *testing.T) {
	var scalar client.ItemData_Relations_AdditionalProperties
	if err := scalar.FromItemDataRelations0("http://zotero.org/users/1/items/AAAA1111"); err != nil {
		t.Fatal(err)
	}
	var arr client.ItemData_Relations_AdditionalProperties
	if err := arr.FromItemDataRelations1([]string{
		"http://zotero.org/users/1/items/BBBB2222",
		"http://zotero.org/users/1/items/CCCC3333",
	}); err != nil {
		t.Fatal(err)
	}

	got := decodeRelations(map[string]client.ItemData_Relations_AdditionalProperties{
		"dc:replaces": scalar,
		"dc:relation": arr,
	})

	if want := []string{"http://zotero.org/users/1/items/AAAA1111"}; !slices.Equal(got["dc:replaces"], want) {
		t.Errorf("scalar form decoded to %v, want %v", got["dc:replaces"], want)
	}
	if len(got["dc:relation"]) != 2 {
		t.Errorf("array form decoded to %v, want 2 entries", got["dc:relation"])
	}
}

func TestDecodeRelations_NilIsEmptyNotPanic(t *testing.T) {
	if got := decodeRelations(nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// Round-tripping must preserve every predicate, including ones sci does not
// manage: a PATCH sends the whole relations object, so dropping owl:sameAs
// while adding a dc:relation would silently delete Zotero's own links.
func TestRelationsRoundTrip_PreservesForeignPredicates(t *testing.T) {
	in := map[string][]string{
		"owl:sameAs":  {"http://zotero.org/groups/9/items/ZZZZ9999"},
		"dc:relation": {"http://zotero.org/users/1/items/AAAA1111"},
	}
	got := decodeRelations(encodeRelations(in))

	for pred, want := range in {
		if !slices.Equal(got[pred], want) {
			t.Errorf("predicate %s: got %v, want %v", pred, got[pred], want)
		}
	}
}

func TestEncodeRelations_EmptyYieldsEmptyMapNotNil(t *testing.T) {
	// An item whose last relation was removed must PATCH `relations: {}` to
	// actually clear it; a nil map would marshal away under omitempty and
	// leave the old relations in place on the server.
	got := encodeRelations(map[string][]string{})
	if got == nil {
		t.Fatal("encodeRelations returned nil; a cleared relation set must survive as {}")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// relationsServer is a minimal fake Zotero that serves each item's current
// relations and records every PATCH, so a test can assert on what actually
// reached the wire per item.
type relationsServer struct {
	mu      sync.Mutex
	stored  map[string]map[string][]string // itemKey → predicate → URIs
	patched map[string]int                 // itemKey → PATCH count
}

func newRelationsServer(initial map[string]map[string][]string) *relationsServer {
	if initial == nil {
		initial = map[string]map[string][]string{}
	}
	return &relationsServer{stored: initial, patched: map[string]int{}}
}

func (s *relationsServer) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := path.Base(r.URL.Path)
		s.mu.Lock()
		defer s.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			rels := encodeRelations(s.stored[key])
			it := client.Item{Data: client.ItemData{
				Key:       &key,
				ItemType:  "journalArticle",
				Version:   new(1),
				Relations: &rels,
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(it); err != nil {
				t.Errorf("encode item: %v", err)
			}
		case http.MethodPatch:
			var body client.ItemData
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode patch: %v", err)
			}
			if body.Relations == nil {
				t.Errorf("PATCH for %s carried no relations field", key)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			s.stored[key] = decodeRelations(*body.Relations)
			s.patched[key]++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
}

// The load-bearing behavior: Zotero maintains dc:relation reciprocity in
// the CLIENT (relatedBox.js saves both items), not the server. A one-sided
// write shows the relation on one item and not the other, which reads as
// corruption. Verified against zotero/zotero, not assumed.
func TestLinkItems_WritesBothDirections(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(nil)
	c, _ := newTestClient(t, srv.handler(t))

	if err := c.LinkItems(context.Background(), "AAAA1111", "BBBB2222"); err != nil {
		t.Fatal(err)
	}

	forward := srv.stored["AAAA1111"][RelatedPredicate]
	reverse := srv.stored["BBBB2222"][RelatedPredicate]
	if want := itemURI("users/42", "BBBB2222"); !slices.Contains(forward, want) {
		t.Errorf("forward side = %v, want it to contain %q", forward, want)
	}
	if want := itemURI("users/42", "AAAA1111"); !slices.Contains(reverse, want) {
		t.Errorf("reverse side = %v, want it to contain %q — a one-sided link is invisible on the other item", reverse, want)
	}
}

// A PATCH replaces the whole relations object, so the read-modify-write has
// to carry predicates sci never manages. Dropping them would silently
// delete Zotero's own owl:sameAs links between group and personal copies.
func TestLinkItems_PreservesZoteroOwnedPredicates(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(map[string]map[string][]string{
		"AAAA1111": {
			"owl:sameAs":  {"http://zotero.org/groups/9/items/ZZZZ9999"},
			"dc:replaces": {"http://zotero.org/users/42/items/YYYY8888"},
		},
	})
	c, _ := newTestClient(t, srv.handler(t))

	if err := c.LinkItems(context.Background(), "AAAA1111", "BBBB2222"); err != nil {
		t.Fatal(err)
	}

	got := srv.stored["AAAA1111"]
	if len(got["owl:sameAs"]) != 1 {
		t.Errorf("owl:sameAs was clobbered: %v", got["owl:sameAs"])
	}
	if len(got["dc:replaces"]) != 1 {
		t.Errorf("dc:replaces was clobbered: %v", got["dc:replaces"])
	}
	if len(got[RelatedPredicate]) != 1 {
		t.Errorf("dc:relation not added: %v", got[RelatedPredicate])
	}
}

// Re-linking an existing pair must not write, so a retry after a partial
// failure repairs the missing side instead of duplicating the present one.
func TestLinkItems_IsIdempotent(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(nil)
	c, _ := newTestClient(t, srv.handler(t))

	for range 2 {
		if err := c.LinkItems(context.Background(), "AAAA1111", "BBBB2222"); err != nil {
			t.Fatal(err)
		}
	}

	if got := srv.stored["AAAA1111"][RelatedPredicate]; len(got) != 1 {
		t.Errorf("forward side = %v, want exactly one URI after two links", got)
	}
	if n := srv.patched["AAAA1111"]; n != 1 {
		t.Errorf("PATCHed AAAA1111 %d times, want 1 — the second link should be a no-op", n)
	}
}

func TestUnlinkItems_RemovesBothDirections(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(map[string]map[string][]string{
		"AAAA1111": {RelatedPredicate: {itemURI("users/42", "BBBB2222")}},
		"BBBB2222": {RelatedPredicate: {itemURI("users/42", "AAAA1111")}},
	})
	c, _ := newTestClient(t, srv.handler(t))

	if err := c.UnlinkItems(context.Background(), "AAAA1111", "BBBB2222"); err != nil {
		t.Fatal(err)
	}

	if got := srv.stored["AAAA1111"][RelatedPredicate]; len(got) != 0 {
		t.Errorf("forward side still %v", got)
	}
	if got := srv.stored["BBBB2222"][RelatedPredicate]; len(got) != 0 {
		t.Errorf("reverse side still %v — an unlink that clears one side leaves a dangling relation", got)
	}
}

func TestLinkItems_RejectsSelfLink(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(nil)
	c, _ := newTestClient(t, srv.handler(t))

	if err := c.LinkItems(context.Background(), "AAAA1111", "AAAA1111"); err == nil {
		t.Error("expected an error relating an item to itself")
	}
	if len(srv.patched) != 0 {
		t.Errorf("a rejected self-link still wrote: %v", srv.patched)
	}
}

func TestItemRelations_ReturnsKeysNotURIs(t *testing.T) {
	t.Parallel()
	srv := newRelationsServer(map[string]map[string][]string{
		"AAAA1111": {
			RelatedPredicate: {
				itemURI("users/42", "BBBB2222"),
				"mailto:not-an-item", // must be skipped, not fail the listing
			},
		},
	})
	c, _ := newTestClient(t, srv.handler(t))

	got, err := c.ItemRelations(context.Background(), "AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"BBBB2222"}; !slices.Equal(got[RelatedPredicate], want) {
		t.Errorf("got %v, want %v", got[RelatedPredicate], want)
	}
}
