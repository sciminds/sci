package cloud

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// TestR2Integration performs a live upload → list → download → delete cycle
// against the real R2 bucket. Skipped unless SCI_R2_INTEGRATION=1 is set.
func TestR2Integration(t *testing.T) {
	if os.Getenv("SCI_R2_INTEGRATION") != "1" {
		t.Skip("set SCI_R2_INTEGRATION=1 to run live R2 tests")
	}

	cfg, err := RequireConfig()
	if err != nil {
		t.Fatalf("RequireConfig: %v", err)
	}
	c := NewClient(cfg.AccountID, cfg.Username, cfg.Public)
	ctx := context.Background()

	const filename = "_test_integration.csv"
	const content = "col_a,col_b\n1,2\n3,4\n"

	// Cleanup on exit regardless of pass/fail.
	t.Cleanup(func() {
		_ = c.Delete(ctx, filename)
	})

	// Upload
	if err := c.Upload(ctx, filename, bytes.NewReader([]byte(content)), "text/csv", nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	url := c.PublicObjectURL(filename)
	t.Logf("uploaded: %s", url)
	if url == "" {
		t.Fatal("PublicObjectURL returned empty string")
	}

	// Exists
	exists, err := c.Exists(ctx, filename)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists returned false after upload")
	}

	// List
	objects, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, o := range objects {
		if o.Key == cfg.Username+"/"+filename {
			found = true
			if o.Size != int64(len(content)) {
				t.Errorf("listed size = %d, want %d", o.Size, len(content))
			}
		}
	}
	if !found {
		t.Errorf("uploaded object not found in List (looked for %s/%s among %d objects)", cfg.Username, filename, len(objects))
	}

	// Download
	var buf bytes.Buffer
	if err := c.Download(ctx, filename, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != content {
		t.Errorf("Download content = %q, want %q", buf.String(), content)
	}

	// Delete
	if err := c.Delete(ctx, filename); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify gone
	exists, err = c.Exists(ctx, filename)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("object still exists after delete")
	}
}
