package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHashPDF_Format verifies the fingerprint is "<size>-<mtime>".
func TestHashPDF_Format(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.pdf")
	content := []byte("hello world")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().Unix())
	got, err := HashPDF(p)
	if err != nil {
		t.Fatal(err)
	}
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

// TestHashPDF_DifferentSizeDifferentHash verifies different file sizes
// produce different fingerprints.
func TestHashPDF_DifferentSizeDifferentHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("much longer content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ha, _ := HashPDF(a)
	hb, _ := HashPDF(b)
	if ha == hb {
		t.Errorf("distinct files produced same fingerprint %q", ha)
	}
}

// TestHashPDF_DifferentMtimeDifferentHash verifies that touching a file
// (changing mtime without changing content) changes the fingerprint.
func TestHashPDF_DifferentMtimeDifferentHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "file.pdf")
	if err := os.WriteFile(p, []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, _ := HashPDF(p)

	// Shift mtime back by 10 seconds.
	past := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	h2, _ := HashPDF(p)
	if h1 == h2 {
		t.Errorf("different mtime produced same fingerprint %q", h1)
	}
}
