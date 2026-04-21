package api

import (
	"testing"
	"time"
)

func TestComputeFileDigest_MD5SizeAndEpochMillis(t *testing.T) {
	t.Parallel()

	// "hello" has a well-known MD5 we can pin without importing crypto.
	const helloMD5 = "5d41402abc4b2a76b9719d911017c592"
	body := []byte("hello")
	mod := time.Date(2025, 3, 14, 12, 0, 0, 0, time.UTC)

	d := computeFileDigest(body, mod)

	if d.MD5 != helloMD5 {
		t.Errorf("md5 = %q, want %q", d.MD5, helloMD5)
	}
	if d.Size != len(body) {
		t.Errorf("size = %d, want %d", d.Size, len(body))
	}
	wantMillis := mod.UnixMilli()
	if int64(d.MTimeMillis) != wantMillis {
		t.Errorf("mtime = %d, want %d (unix millis)", d.MTimeMillis, wantMillis)
	}
}

func TestComputeFileDigest_EmptyFile(t *testing.T) {
	t.Parallel()
	// Empty input still gets a canonical 32-char lowercase-hex MD5
	// (d41d8cd98f00b204e9800998ecf8427e). Zotero's spec enforces that format.
	const emptyMD5 = "d41d8cd98f00b204e9800998ecf8427e"
	d := computeFileDigest(nil, time.Unix(0, 0))
	if d.MD5 != emptyMD5 {
		t.Errorf("md5 = %q, want %q", d.MD5, emptyMD5)
	}
	if d.Size != 0 {
		t.Errorf("size = %d, want 0", d.Size)
	}
}
