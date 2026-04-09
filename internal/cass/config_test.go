package cass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCanvasURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantBase string
		wantID   int
		wantErr  bool
	}{
		{
			name:     "standard URL",
			url:      "https://canvas.ucsd.edu/courses/63653",
			wantBase: "https://canvas.ucsd.edu",
			wantID:   63653,
		},
		{
			name:     "URL with trailing slash",
			url:      "https://canvas.ucsd.edu/courses/63653/",
			wantBase: "https://canvas.ucsd.edu",
			wantID:   63653,
		},
		{
			name:     "URL with subpath",
			url:      "https://canvas.ucsd.edu/courses/63653/assignments",
			wantBase: "https://canvas.ucsd.edu",
			wantID:   63653,
		},
		{
			name:     "URL with query params",
			url:      "https://canvas.ucsd.edu/courses/63653?foo=bar",
			wantBase: "https://canvas.ucsd.edu",
			wantID:   63653,
		},
		{
			name:    "missing courses path",
			url:     "https://canvas.ucsd.edu/users/123",
			wantErr: true,
		},
		{
			name:    "non-numeric course ID",
			url:     "https://canvas.ucsd.edu/courses/abc",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, id, err := ParseCanvasURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if base != tt.wantBase {
				t.Errorf("base = %q, want %q", base, tt.wantBase)
			}
			if id != tt.wantID {
				t.Errorf("id = %d, want %d", id, tt.wantID)
			}
		})
	}
}

func TestParseClassroomURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  int
		wantErr bool
	}{
		{
			name:   "standard URL",
			url:    "https://classroom.github.com/classrooms/299058",
			wantID: 299058,
		},
		{
			name:   "URL with slug suffix",
			url:    "https://classroom.github.com/classrooms/299058-psyc-201",
			wantID: 299058,
		},
		{
			name:   "URL with query params",
			url:    "https://classroom.github.com/classrooms/299058?tab=assignments",
			wantID: 299058,
		},
		{
			name:    "wrong host",
			url:     "https://github.com/classrooms/299058",
			wantErr: true,
		},
		{
			name:    "missing classrooms path",
			url:     "https://classroom.github.com/assignments/123",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseClassroomURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id = %d, want %d", id, tt.wantID)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()

	t.Run("canvas only", func(t *testing.T) {
		path := filepath.Join(dir, "canvas-only.yaml")
		data := []byte("canvas:\n  url: https://canvas.ucsd.edu/courses/63653\n")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Canvas.URL != "https://canvas.ucsd.edu/courses/63653" {
			t.Errorf("canvas url = %q", cfg.Canvas.URL)
		}
		if cfg.HasClassroom() {
			t.Error("expected no classroom")
		}
		base, id, err := cfg.CanvasParts()
		if err != nil {
			t.Fatal(err)
		}
		if base != "https://canvas.ucsd.edu" || id != 63653 {
			t.Errorf("parts = (%q, %d)", base, id)
		}
	})

	t.Run("canvas plus classroom", func(t *testing.T) {
		path := filepath.Join(dir, "both.yaml")
		data := []byte("canvas:\n  url: https://canvas.ucsd.edu/courses/63653\nclassroom:\n  url: https://classroom.github.com/classrooms/299058\n")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.HasClassroom() {
			t.Error("expected classroom")
		}
		id, err := cfg.ClassroomURLID()
		if err != nil {
			t.Fatal(err)
		}
		if id != 299058 {
			t.Errorf("classroom id = %d", id)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadConfig(filepath.Join(dir, "nope.yaml"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := filepath.Join(dir, "bad.yaml")
		if err := os.WriteFile(path, []byte(":::"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(path)
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
	})
}

func TestFindConfig(t *testing.T) {
	// Create nested dirs with cass.yaml at the root level
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, "cass.yaml")
	data := []byte("canvas:\n  url: https://canvas.ucsd.edu/courses/1\n")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("finds config walking up", func(t *testing.T) {
		found, err := FindConfig(sub)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found != configPath {
			t.Errorf("found = %q, want %q", found, configPath)
		}
	})

	t.Run("no config found", func(t *testing.T) {
		empty := t.TempDir()
		_, err := FindConfig(empty)
		if err == nil {
			t.Fatal("expected error when no cass.yaml found")
		}
	})
}

func TestCanvasToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	t.Run("save and load", func(t *testing.T) {
		if err := SaveCanvasToken(path, "test-token-123"); err != nil {
			t.Fatalf("save: %v", err)
		}

		token, err := LoadCanvasToken(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if token != "test-token-123" {
			t.Errorf("token = %q", token)
		}
	})

	t.Run("require missing", func(t *testing.T) {
		_, err := RequireCanvasToken(filepath.Join(dir, "nope.json"))
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("require empty", func(t *testing.T) {
		emptyPath := filepath.Join(dir, "empty.json")
		if err := SaveCanvasToken(emptyPath, ""); err != nil {
			t.Fatal(err)
		}
		_, err := RequireCanvasToken(emptyPath)
		if err == nil {
			t.Fatal("expected error for empty token")
		}
	})

	t.Run("file permissions", func(t *testing.T) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		perm := info.Mode().Perm()
		if perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	})
}
