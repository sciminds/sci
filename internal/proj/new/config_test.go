package new

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncResultJSON(t *testing.T) {
	r := SyncResult{
		Dir:    "/tmp/proj",
		DryRun: true,
		Changed: []SyncChange{
			{Path: ".vscode/settings.json", Changed: true, Exists: true},
			{Path: ".zed/settings.json", Changed: false, Exists: true},
		},
	}
	data, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dryRun":true`) {
		t.Errorf("expected dryRun in JSON: %s", data)
	}
}

func TestSyncResultHuman_NoChanges(t *testing.T) {
	r := SyncResult{
		Changed: []SyncChange{
			{Path: ".vscode/settings.json", Changed: false},
		},
	}
	got := r.Human()
	if !strings.Contains(got, "up to date") {
		t.Errorf("expected 'up to date' message, got %q", got)
	}
}

func TestSyncResultHuman_WithChanges(t *testing.T) {
	r := SyncResult{
		Changed: []SyncChange{
			{Path: ".vscode/settings.json", Changed: true},
			{Path: ".zed/settings.json", Changed: false},
		},
	}
	got := r.Human()
	if !strings.Contains(got, "1 file(s) updated") {
		t.Errorf("expected '1 file(s) updated', got %q", got)
	}
}

func TestSyncResultHuman_DryRun(t *testing.T) {
	r := SyncResult{
		DryRun: true,
		Changed: []SyncChange{
			{Path: ".vscode/settings.json", Changed: true},
		},
	}
	got := r.Human()
	if !strings.Contains(got, "would be updated") {
		t.Errorf("expected 'would be updated', got %q", got)
	}
}

func TestApplyConfigFiles(t *testing.T) {
	dir := t.TempDir()
	files := []ConfigFile{
		{Path: ".vscode/settings.json", Content: `{"editor.fontSize": 14}`},
		{Path: ".zed/tasks.json", Content: `[]`},
	}

	if err := ApplyConfigFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	// Verify files were written.
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Errorf("reading %s: %v", f.Path, err)
			continue
		}
		if string(data) != f.Content {
			t.Errorf("%s: got %q, want %q", f.Path, string(data), f.Content)
		}
	}
}

func TestManagedFiles_NonEmpty(t *testing.T) {
	if len(ManagedFiles) == 0 {
		t.Error("ManagedFiles should not be empty")
	}
	for _, f := range ManagedFiles {
		if !strings.HasSuffix(f, ".tmpl") {
			t.Errorf("ManagedFile %q should have .tmpl extension", f)
		}
	}
}
