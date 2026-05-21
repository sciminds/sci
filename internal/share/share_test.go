package share

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cloud"
)

// fakeUploader implements dirUploader for shareDir unit tests.
type fakeUploader struct {
	prefixHas        bool
	prefixErr        error
	prefixCalledWith string
	syncErr          error
	syncLocal        string
	syncName         string
	syncCalled       bool
}

func (f *fakeUploader) PrefixExists(_ context.Context, name string) (bool, error) {
	f.prefixCalledWith = name
	return f.prefixHas, f.prefixErr
}

func (f *fakeUploader) SyncUp(_ context.Context, localDir, name string) error {
	f.syncCalled = true
	f.syncLocal = localDir
	f.syncName = name
	return f.syncErr
}

func TestBucketFor(t *testing.T) {
	if got := BucketFor(true); got != cloud.BucketPublic {
		t.Errorf("BucketFor(true) = %q, want %q", got, cloud.BucketPublic)
	}
	if got := BucketFor(false); got != cloud.BucketPrivate {
		t.Errorf("BucketFor(false) = %q, want %q", got, cloud.BucketPrivate)
	}
}

func TestDefaultShareName(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "results.csv")
	if err := os.WriteFile(f, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DefaultShareName(f); got != "results.csv" {
		t.Errorf("DefaultShareName(file) = %q, want %q", got, "results.csv")
	}

	if got := DefaultShareName("/no/such/file.db"); got != "file.db" {
		t.Errorf("DefaultShareName(missing) = %q, want %q", got, "file.db")
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data.csv", "csv"},
		{"data.tsv", "csv"},
		{"backup.db", "db"},
		{"photo.png", "media"},
		{"archive.zip", "zip"},
		{"readme.txt", "other"},
	}
	for _, tt := range tests {
		if got := DetectFileType(tt.path); got != tt.want {
			t.Errorf("DetectFileType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDetectFileType_UppercaseExtensions(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"DATA.CSV", "csv"},
		{"PHOTO.PNG", "media"},
		{"ARCHIVE.ZIP", "zip"},
		{"FILE.DB", "db"},
		{"Data.Tsv", "csv"},
	}
	for _, tt := range tests {
		if got := DetectFileType(tt.path); got != tt.want {
			t.Errorf("DetectFileType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDefaultShareName_SpecialCharsInFilename(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "my data (final).csv")
	if err := os.WriteFile(f, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DefaultShareName(f)
	if got != "my data (final).csv" {
		t.Errorf("DefaultShareName = %q, want %q", got, "my data (final).csv")
	}
}

func TestNameFromFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/results.csv", "results"},
		{"data.tar.gz", "data.tar"},
		{"mydir", "mydir"},
	}
	for _, tt := range tests {
		if got := nameFromFile(tt.path); got != tt.want {
			t.Errorf("nameFromFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestResolveDownloadKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc, name, username string
		public               bool
		want                 string
	}{
		// Private bucket: arg is always relative to the user's folder
		// unless they typed their own prefix explicitly.
		{"priv: bare filename auto-prefixes", "foo.csv", "ejolly", false, "ejolly/foo.csv"},
		{"priv: nested path stays under user", "tutorials/data/credit.csv", "ejolly", false, "ejolly/tutorials/data/credit.csv"},
		{"priv: explicit own prefix passes through", "ejolly/sub/x.csv", "ejolly", false, "ejolly/sub/x.csv"},
		{"priv: foreign-looking prefix treated as own subfolder", "alice/x.csv", "ejolly", false, "ejolly/alice/x.csv"},
		// Public bucket: bare still maps to your own folder; any other
		// "/" path is taken as an absolute key (cross-user form).
		{"pub: bare filename auto-prefixes", "foo.csv", "ejolly", true, "ejolly/foo.csv"},
		{"pub: explicit own prefix passes through", "ejolly/x.csv", "ejolly", true, "ejolly/x.csv"},
		{"pub: cross-user form preserved", "alice/x.csv", "ejolly", true, "alice/x.csv"},
		{"pub: leading slash trimmed", "/foo.csv", "ejolly", true, "ejolly/foo.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := resolveDownloadKey(tc.name, tc.username, tc.public); got != tc.want {
				t.Errorf("resolveDownloadKey(%q, %q, public=%v) = %q, want %q",
					tc.name, tc.username, tc.public, got, tc.want)
			}
		})
	}
}

// ── shareDir ────────────────────────────────────────────────────────────────

func TestShareDir_NoForce_RefusesNonEmptyPrefix(t *testing.T) {
	up := &fakeUploader{prefixHas: true}
	_, err := shareDir(up, cloud.BucketPrivate, "/tmp/x", "myrepo", false)
	if err == nil {
		t.Fatal("shareDir = nil err on non-empty prefix; want error")
	}
	if !strings.Contains(err.Error(), "myrepo") {
		t.Errorf("err = %v, want it to name the prefix", err)
	}
	if up.syncCalled {
		t.Error("SyncUp called despite non-empty prefix refusal")
	}
}

func TestShareDir_Force_SkipsPrefixCheckAndSyncs(t *testing.T) {
	up := &fakeUploader{prefixHas: true} // even if true, force ignores
	res, err := shareDir(up, cloud.BucketPrivate, "/tmp/x", "myrepo", true)
	if err != nil {
		t.Fatal(err)
	}
	if up.prefixCalledWith != "" {
		t.Errorf("PrefixExists called with force=true; want skipped")
	}
	if !up.syncCalled {
		t.Error("SyncUp not called under force")
	}
	if up.syncLocal != "/tmp/x" || up.syncName != "myrepo" {
		t.Errorf("SyncUp args = (%q, %q), want (/tmp/x, myrepo)", up.syncLocal, up.syncName)
	}
	if res == nil || !res.OK || res.Action != "put" {
		t.Errorf("CloudResult = %+v, want OK put result", res)
	}
}

func TestShareDir_EmptyPrefix_SyncsSuccessfully(t *testing.T) {
	up := &fakeUploader{} // prefixHas defaults to false
	res, err := shareDir(up, cloud.BucketPrivate, "/tmp/x", "myrepo", false)
	if err != nil {
		t.Fatal(err)
	}
	if up.prefixCalledWith != "myrepo" {
		t.Errorf("PrefixExists called with %q, want myrepo", up.prefixCalledWith)
	}
	if !up.syncCalled {
		t.Error("SyncUp not called after empty-prefix check")
	}
	if res == nil || res.Message == "" {
		t.Errorf("missing CloudResult message: %+v", res)
	}
}

func TestShareDir_SyncError_Propagated(t *testing.T) {
	up := &fakeUploader{syncErr: errors.New("hf timeout")}
	_, err := shareDir(up, cloud.BucketPrivate, "/tmp/x", "myrepo", true)
	if err == nil {
		t.Fatal("shareDir = nil err despite SyncUp error")
	}
	if !strings.Contains(err.Error(), "hf timeout") {
		t.Errorf("err = %v, want to contain 'hf timeout'", err)
	}
}

func TestDefaultShareName_Dir_NoZipSuffix(t *testing.T) {
	tmp := t.TempDir()
	d := filepath.Join(tmp, "mydata")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DefaultShareName(d); got != "mydata" {
		t.Errorf("DefaultShareName(dir) = %q, want %q (no .zip suffix; uploaded as tree)", got, "mydata")
	}
}

func TestTreeToShared_FoldersAndFiles(t *testing.T) {
	entries := []TreeEntry{
		{Name: "data", Key: "ejolly/data", IsDir: true},
		{Name: "results.csv", Key: "ejolly/results.csv", Size: 1024, URL: "https://hf.co/ejolly/results.csv", LastModified: "2024-01-01"},
	}
	out := treeToShared(entries)
	if len(out) != 2 {
		t.Fatalf("got %d shared entries, want 2", len(out))
	}
	if !out[0].IsDir || out[0].Name != "data" || out[0].Owner != "ejolly" {
		t.Errorf("folder row = %+v, want IsDir+name=data+owner=ejolly", out[0])
	}
	if out[0].Type != "" {
		t.Errorf("folder rows should leave Type empty, got %q", out[0].Type)
	}
	if out[1].IsDir || out[1].Type != "csv" || out[1].Size != 1024 {
		t.Errorf("file row = %+v, want csv/1024", out[1])
	}
	if out[1].Owner != "ejolly" || out[1].URL == "" {
		t.Errorf("file row should preserve owner+url, got %+v", out[1])
	}
}
