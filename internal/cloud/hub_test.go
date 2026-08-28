package cloud

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hubStub stands up a fake Hub, points HF_ENDPOINT at it, and records the
// paths (with query) each request asked for.
func hubStub(t *testing.T, h http.HandlerFunc) *[]string {
	t.Helper()
	paths := new([]string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.RequestURI())
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HF_ENDPOINT", srv.URL)
	return paths
}

func TestListBucketTree_RequestShape(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	var gotAuth string
	paths := hubStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "private", "alice/my data/"); err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(*paths) != 1 {
		t.Fatalf("made %d requests, want 1", len(*paths))
	}
	got := (*paths)[0]
	// The prefix is one path segment, so a space in it must be escaped rather
	// than splitting the URL, and the trailing slash must not double up.
	if want := "/api/buckets/sciminds/private/tree/alice%2Fmy%20data?recursive=true"; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
	if want := "Bearer hf_abc"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestListBucketTree_NoPrefixListsWholeBucket(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	paths := hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "public", ""); err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if want := "/api/buckets/sciminds/public/tree?recursive=true"; (*paths)[0] != want {
		t.Errorf("request = %q, want %q", (*paths)[0], want)
	}
}

func TestListBucketTree_FollowsPagination(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?cursor=p2>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"type":"file","path":"a.csv","size":1}]`))
		case "p2":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?cursor=p3>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"type":"file","path":"b.csv","size":2}]`))
		default:
			_, _ = w.Write([]byte(`[{"type":"file","path":"c.csv","size":3}]`))
		}
	}))
	t.Cleanup(srv.Close)
	base = srv.URL
	t.Setenv("HF_ENDPOINT", srv.URL)

	files, err := listBucketTree(context.Background(), "o", "b", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3 (all pages concatenated)", len(files))
	}
	if files[0].Path != "a.csv" || files[2].Path != "c.csv" {
		t.Errorf("files = %+v, want page order preserved", files)
	}
}

func TestListBucketTree_MissingPrefixIsEmptyNotAnError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Entry not found"}`))
	})

	files, err := listBucketTree(context.Background(), "sciminds", "private", "ghost/")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v, want nil for an absent prefix", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %+v, want empty", files)
	}
}

func TestListBucketTree_ServerErrorSurfaces(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "private", ""); err == nil {
		t.Fatal("listBucketTree() error = nil, want the 500 surfaced")
	}
}

func TestListBucketTree_ParsesAPIFieldNames(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	// The Hub answers in camelCase; the `hf buckets ls --json` output this
	// replaced used snake_case, so the tags must track the API.
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"type":"file","path":"alice/a.csv","size":12,` +
			`"mtime":"2026-05-01T00:00:00.000Z","uploadedAt":"2026-05-02T00:00:00.000Z"}]`))
	})

	files, err := listBucketTree(context.Background(), "sciminds", "private", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.Type != "file" || f.Path != "alice/a.csv" || f.Size != 12 {
		t.Errorf("file = %+v", f)
	}
	if !strings.HasPrefix(f.MTime, "2026-05-01") {
		t.Errorf("MTime = %q, want the mtime field decoded", f.MTime)
	}
	if !strings.HasPrefix(f.UploadedAt, "2026-05-02") {
		t.Errorf("UploadedAt = %q, want the camelCase uploadedAt field decoded", f.UploadedAt)
	}
}
