package share

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/cloud"
)

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

	d := filepath.Join(tmp, "mydata")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DefaultShareName(d); got != "mydata.zip" {
		t.Errorf("DefaultShareName(dir) = %q, want %q", got, "mydata.zip")
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
		if got := detectFileType(tt.path); got != tt.want {
			t.Errorf("detectFileType(%q) = %q, want %q", tt.path, got, tt.want)
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
		if got := detectFileType(tt.path); got != tt.want {
			t.Errorf("detectFileType(%q) = %q, want %q", tt.path, got, tt.want)
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
