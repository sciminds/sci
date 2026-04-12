package extract

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHashPDF_FixedBytes locks the hash format: exactly 12 hex chars
// (48 bits) of the sha256 digest. This is what gets embedded in the
// sentinel comment so any change breaks drift detection across
// extractions.
func TestHashPDF_FixedBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.pdf")
	// Arbitrary fixed bytes — `echo -n "hello world"` — so the digest
	// is a stable golden.
	if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashPDF(p)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	// First 12 hex chars:       b94d27b9934d
	const want = "b94d27b9934d"
	if got != want {
		t.Errorf("HashPDF = %q, want %q", got, want)
	}
}

func TestHashPDF_Missing(t *testing.T) {
	t.Parallel()
	_, err := HashPDF(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// TestHashPDF_DifferentContentDifferentHash makes sure a 1-byte change
// produces a different digest — guards against accidental truncation
// of the input stream.
func TestHashPDF_DifferentContentDifferentHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("version 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("version 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	ha, _ := HashPDF(a)
	hb, _ := HashPDF(b)
	if ha == hb {
		t.Errorf("distinct contents produced same hash %q", ha)
	}
}
