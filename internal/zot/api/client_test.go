package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/sciminds/sci/internal/zot"
)

var (
	personal = zot.LibraryRef{Scope: zot.LibPersonal, Name: "Personal"}
	shared   = zot.LibraryRef{Scope: zot.LibShared, Name: "sciminds"}
)

func TestNew_RejectsMissingAPIKey(t *testing.T) {
	t.Parallel()
	_, err := New(Credentials{UserID: "42"}, personal)
	if err == nil {
		t.Fatal("New() error = nil, want a missing-key error")
	}
}

func TestNew_RejectsMissingOrNonNumericUserID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "alice", "4 2", "-5", "+5"} {
		if _, err := New(Credentials{APIKey: "k", UserID: id}, personal); err == nil {
			t.Errorf("New(UserID=%q) error = nil, want a user-id error", id)
		}
	}
}

func TestNew_RejectsLibAll(t *testing.T) {
	t.Parallel()
	_, err := New(testCreds, zot.LibraryRef{Scope: zot.LibAll})
	if err == nil {
		t.Fatal("New(LibAll) error = nil, want an error: a write targets one library")
	}
}

func TestNew_SharedRequiresNumericGroupID(t *testing.T) {
	t.Parallel()
	for _, gid := range []string{"", "sciminds"} {
		creds := Credentials{APIKey: "k", UserID: "42", GroupID: gid}
		if _, err := New(creds, shared); err == nil {
			t.Errorf("New(shared, GroupID=%q) error = nil, want a group-id error", gid)
		}
	}
	if _, err := New(Credentials{APIKey: "k", UserID: "42"}, personal); err != nil {
		t.Errorf("New(personal, no GroupID) error = %v, want nil", err)
	}
}

func TestNew_DefaultsAndLibrary(t *testing.T) {
	t.Parallel()
	c, err := New(testCreds, shared)
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "https://api.zotero.org" {
		t.Errorf("baseURL = %q, want https://api.zotero.org", c.baseURL)
	}
	if c.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", c.maxRetries)
	}
	hc, ok := c.http.(*http.Client)
	if !ok || hc.Timeout != 30*time.Second {
		t.Errorf("default transport = %#v, want *http.Client with 30s timeout", c.http)
	}
	if c.Library() != shared {
		t.Errorf("Library() = %+v, want %+v", c.Library(), shared)
	}
}

func TestRequest_SendsZoteroHeaders(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.seen()[0]
	if got.Header.Get("Zotero-API-Key") != "test-key" {
		t.Errorf("Zotero-API-Key = %q, want test-key", got.Header.Get("Zotero-API-Key"))
	}
	if got.Header.Get("Zotero-API-Version") != "3" {
		t.Errorf("Zotero-API-Version = %q, want 3", got.Header.Get("Zotero-API-Version"))
	}
}

func TestCurrentKey_HasNoLibraryPrefix(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	info, err := c.CurrentKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GET /keys/current"}; !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want %v", f.methods(), want)
	}
	if info.Key != "test-key" || info.UserID != 42 || info.Username != "alice" {
		t.Errorf("KeyInfo = %+v, want {test-key 42 alice}", *info)
	}
}

// keyServer answers /keys/current with body, for the access-decoding tests.
func keyServer(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func TestCurrentKey_DecodesAccessWithAllGroups(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, keyServer(`{"key":"k","userID":42,"username":"alice",
		"access":{"user":{"library":true,"files":true,"notes":true,"write":true},
		          "groups":{"all":{"library":true,"write":true}}}}`))

	info, err := c.CurrentKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Access.UserLibrary || !info.Access.UserWrite {
		t.Errorf("user access = %+v, want library and write true", info.Access)
	}
	all, ok := info.Access.Groups["all"]
	if !ok || !all.Library || !all.Write {
		t.Errorf("Groups[all] = %+v (present=%v), want library and write true", all, ok)
	}
	if !info.Access.GroupWrite("777") {
		t.Error("GroupWrite(777) = false, want true via the \"all\" entry")
	}
}

func TestCurrentKey_DecodesAccessKeyedByGroupID(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, keyServer(`{"key":"k","userID":42,"username":"alice",
		"access":{"user":{"library":true,"write":false},
		          "groups":{"777":{"library":true,"write":true},"888":{"library":true,"write":false}}}}`))

	info, err := c.CurrentKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Access.UserWrite {
		t.Error("UserWrite = true, want false")
	}
	if got := info.Access.Groups["777"]; got != (GroupAccess{Library: true, Write: true}) {
		t.Errorf("Groups[777] = %+v, want library+write", got)
	}
	if info.Access.GroupWrite("888") {
		t.Error("GroupWrite(888) = true, want false: that group is read-only")
	}
	if info.Access.GroupWrite("999") {
		t.Error("GroupWrite(999) = true, want false: the key names no such group")
	}
	if !info.Access.GroupWrite("777") {
		t.Error("GroupWrite(777) = false, want true")
	}
}

func TestCurrentKey_AbsentAccessIsFalse(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"no access block": `{"key":"k","userID":42,"username":"alice"}`,
		"empty access":    `{"key":"k","userID":42,"username":"alice","access":{}}`,
		"user only":       `{"key":"k","userID":42,"username":"alice","access":{"user":{"library":true}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestClient(t, keyServer(body))
			info, err := c.CurrentKey(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.Access.UserWrite || info.Access.GroupWrite("777") || info.Access.GroupWrite("all") {
				t.Errorf("Access = %+v, want no write access anywhere", info.Access)
			}
		})
	}
}

func TestLibraryPrefix_PersonalUsesUsers(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 5, map[string]any{"itemType": "book"})
	c, _ := newTestClient(t, f)

	if _, err := c.GetItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatal(err)
	}
	if got := f.seen()[0].Path; got != "/users/42/items/ABCD1234" {
		t.Errorf("path = %q, want /users/42/items/ABCD1234", got)
	}
}

func TestLibraryPrefix_SharedUsesGroups(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.prefix = "/groups/777"
	f.seedItem("ABCD1234", 5, map[string]any{"itemType": "book"})
	c, _ := newTestClientFor(t, f, shared)

	if _, err := c.GetItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatal(err)
	}
	if got := f.seen()[0].Path; got != "/groups/777/items/ABCD1234" {
		t.Errorf("path = %q, want /groups/777/items/ABCD1234", got)
	}
}

func TestUnexpectedStatus_IsHTTPError(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
	c, _ := newTestClient(t, h)

	_, err := c.CurrentKey(context.Background())
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want *HTTPError", err, err)
	}
	if herr.Status != http.StatusForbidden || herr.Method != http.MethodGet || herr.Path != "/keys/current" {
		t.Errorf("HTTPError = %+v, want GET /keys/current 403", *herr)
	}
	if herr.Body != "Forbidden" {
		t.Errorf("Body = %q, want Forbidden", herr.Body)
	}
}

func TestRequest_HonoursContextCancellation(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, log := newTestClient(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.CurrentKey(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(log.sleeps) != 0 {
		t.Errorf("slept %v on a cancelled context, want no retries", log.sleeps)
	}
}

func equalStrings(a, b []string) bool { return slices.Equal(a, b) }
