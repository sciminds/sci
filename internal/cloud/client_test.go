package cloud

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// fakeRun records the args of each call and returns canned output.
type fakeRun struct {
	calls [][]string
	out   []byte
	err   error
}

func (f *fakeRun) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, slices.Clone(args))
	return f.out, f.err
}

// fakeStream records args and stdin/stdout interactions.
type fakeStream struct {
	calls    [][]string
	stdinHas []byte
	stdoutAs []byte
	err      error
}

func (f *fakeStream) stream(_ context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	f.calls = append(f.calls, slices.Clone(args))
	if stdin != nil {
		buf, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		f.stdinHas = buf
	}
	if stdout != nil && len(f.stdoutAs) > 0 {
		if _, err := stdout.Write(f.stdoutAs); err != nil {
			return err
		}
	}
	return f.err
}

// newTestClient builds a Client wired to fake run/stream hooks.
func newTestClient(t *testing.T, bucket string) (*Client, *fakeRun, *fakeStream) {
	t.Helper()
	r, s := &fakeRun{}, &fakeStream{}
	c := &Client{
		Username: "alice",
		Org:      DefaultOrg,
		Bucket:   bucket,
		run:      r.run,
		stream:   s.stream,
	}
	return c, r, s
}

func TestClient_BucketHandle(t *testing.T) {
	c, _, _ := newTestClient(t, BucketPublic)
	if got, want := c.bucketHandle(), "hf://buckets/sciminds/public"; got != want {
		t.Errorf("bucketHandle = %q, want %q", got, want)
	}
}

func TestClient_ObjectKey(t *testing.T) {
	c, _, _ := newTestClient(t, BucketPrivate)
	if got, want := c.objectKey("results.csv"), "alice/results.csv"; got != want {
		t.Errorf("objectKey = %q, want %q", got, want)
	}
}

func TestClient_PublicObjectURL(t *testing.T) {
	pub, _, _ := newTestClient(t, BucketPublic)
	if got, want := pub.PublicObjectURL("results.csv"),
		"https://huggingface.co/buckets/sciminds/public/resolve/alice/results.csv"; got != want {
		t.Errorf("public PublicObjectURL = %q, want %q", got, want)
	}

	priv, _, _ := newTestClient(t, BucketPrivate)
	if got := priv.PublicObjectURL("results.csv"); got != "" {
		t.Errorf("private PublicObjectURL = %q, want empty", got)
	}
}

func TestClient_Upload_StreamsToStdin(t *testing.T) {
	c, _, s := newTestClient(t, BucketPublic)
	body := strings.NewReader("hello,sci\n")

	if err := c.Upload(context.Background(), "data.csv", body); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 {
		t.Fatalf("expected 1 stream call, got %d", len(s.calls))
	}
	want := []string{"buckets", "cp", "-", "hf://buckets/sciminds/public/alice/data.csv"}
	if !strSliceEq(s.calls[0], want) {
		t.Errorf("stream args = %v, want %v", s.calls[0], want)
	}
	if got := string(s.stdinHas); got != "hello,sci\n" {
		t.Errorf("stdin = %q, want %q", got, "hello,sci\n")
	}
}

func TestClient_Download_StreamsFromStdout(t *testing.T) {
	c, _, s := newTestClient(t, BucketPublic)
	s.stdoutAs = []byte("body bytes")

	var buf bytes.Buffer
	if err := c.Download(context.Background(), "data.csv", &buf); err != nil {
		t.Fatal(err)
	}
	want := []string{"buckets", "cp", "hf://buckets/sciminds/public/alice/data.csv", "-"}
	if !strSliceEq(s.calls[0], want) {
		t.Errorf("stream args = %v, want %v", s.calls[0], want)
	}
	if buf.String() != "body bytes" {
		t.Errorf("downloaded body = %q, want %q", buf.String(), "body bytes")
	}
}

func TestClient_DownloadByKey_FullKey(t *testing.T) {
	c, _, s := newTestClient(t, BucketPublic)
	s.stdoutAs = []byte("contents")

	var buf bytes.Buffer
	if err := c.DownloadByKey(context.Background(), "bob/their-file.csv", &buf); err != nil {
		t.Fatal(err)
	}
	want := []string{"buckets", "cp", "hf://buckets/sciminds/public/bob/their-file.csv", "-"}
	if !strSliceEq(s.calls[0], want) {
		t.Errorf("stream args = %v, want %v", s.calls[0], want)
	}
}

func TestClient_List_FiltersToFiles_AndPopulatesPublicURL(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPublic)
	r.out = []byte(`[
		{"type":"directory","path":"alice/sub","size":0,"mtime":"","uploaded_at":""},
		{"type":"file","path":"alice/a.csv","size":12,"mtime":"2026-05-01T00:00:00+00:00","uploaded_at":"2026-05-01T00:01:00+00:00"},
		{"type":"file","path":"alice/sub/b.csv","size":34,"mtime":"2026-05-02T00:00:00+00:00","uploaded_at":"2026-05-02T00:01:00+00:00"}
	]`)

	objs, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2 (directories filtered)", len(objs))
	}
	wantArgs := []string{"buckets", "ls", "sciminds/public/alice", "-R", "--json"}
	if !strSliceEq(r.calls[0], wantArgs) {
		t.Errorf("ls args = %v, want %v", r.calls[0], wantArgs)
	}
	if objs[0].Key != "alice/a.csv" || objs[0].Size != 12 {
		t.Errorf("first object = %+v", objs[0])
	}
	if objs[0].URL != "https://huggingface.co/buckets/sciminds/public/resolve/alice/a.csv" {
		t.Errorf("URL not populated for public bucket: %q", objs[0].URL)
	}
	if objs[0].LastModified != "2026-05-01T00:00:00+00:00" {
		t.Errorf("LastModified = %q", objs[0].LastModified)
	}
}

func TestClient_List_Private_NoURL(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPrivate)
	r.out = []byte(`[{"type":"file","path":"alice/a.csv","size":1,"mtime":"","uploaded_at":""}]`)

	objs, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want 1 object, got %d", len(objs))
	}
	if objs[0].URL != "" {
		t.Errorf("private bucket URL = %q, want empty", objs[0].URL)
	}
}

func TestClient_ListPrefix_Empty(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPublic)
	r.out = []byte("[]")

	objs, err := c.ListPrefix(context.Background(), "alice/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty, got %v", objs)
	}
}

func TestClient_ListPrefix_HFNotFound(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPublic)
	r.out = []byte("error: prefix not found")
	r.err = errors.New("hf failed")

	objs, err := c.ListPrefix(context.Background(), "ghost/")
	if err != nil {
		t.Fatalf("expected nil err for not-found, got %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty, got %v", objs)
	}
}

func TestClient_Exists_True(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPublic)
	r.out = []byte(`[{"type":"file","path":"alice/data.csv","size":1,"mtime":"","uploaded_at":""}]`)

	ok, err := c.Exists(context.Background(), "data.csv")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("Exists = false, want true")
	}
}

func TestClient_Exists_False(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPublic)
	r.out = []byte("[]")

	ok, err := c.Exists(context.Background(), "missing.csv")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("Exists = true, want false")
	}
}

func TestClient_Delete(t *testing.T) {
	c, r, _ := newTestClient(t, BucketPrivate)
	if err := c.Delete(context.Background(), "data.csv"); err != nil {
		t.Fatal(err)
	}
	want := []string{"buckets", "rm", "-y", "sciminds/private/alice/data.csv"}
	if !strSliceEq(r.calls[0], want) {
		t.Errorf("delete args = %v, want %v", r.calls[0], want)
	}
}

func TestParseListing_SkipsDirectories(t *testing.T) {
	out := []byte(`[
		{"type":"directory","path":"alice/sub","size":0,"mtime":"","uploaded_at":""},
		{"type":"file","path":"alice/a.csv","size":1,"mtime":"","uploaded_at":""}
	]`)
	objs, err := parseListing(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Key != "alice/a.csv" {
		t.Errorf("parseListing = %v, want one file alice/a.csv", objs)
	}
}

func TestParseListing_Empty(t *testing.T) {
	objs, err := parseListing([]byte("[]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty, got %v", objs)
	}
}

func TestParseListing_BadJSON(t *testing.T) {
	if _, err := parseListing([]byte("{not json")); err == nil {
		t.Error("expected parse error")
	}
}

func TestIsHFNotFound(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{"nil err", "", nil, false},
		{"not found stderr", "Error: bucket not found", errors.New("exit 1"), true},
		{"no such file", "no such object", errors.New("exit 1"), true},
		{"404 in err", "", errors.New("got 404 from API"), true},
		{"other error", "some random failure", errors.New("exit 1"), false},
	}
	for _, c := range cases {
		got := isHFNotFound([]byte(c.out), c.err)
		if got != c.want {
			t.Errorf("%s: isHFNotFound = %v, want %v", c.name, got, c.want)
		}
	}
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
