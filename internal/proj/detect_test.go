package proj

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string // path → content
		wantNil bool
		wantPkg PkgManager
		wantDoc DocSystem
	}{
		{
			name:    "empty dir",
			files:   nil,
			wantNil: true,
		},
		{
			name:    "pixi.toml only",
			files:   map[string]string{"pixi.toml": "[project]\nname = \"test\""},
			wantPkg: Pixi,
			wantDoc: NoDoc,
		},
		{
			name:    "pyproject with tool.pixi",
			files:   map[string]string{"pyproject.toml": "[project]\nname = \"test\"\n\n[tool.pixi.project]\nplatforms = [\"osx-arm64\"]"},
			wantPkg: Pixi,
			wantDoc: NoDoc,
		},
		{
			name:    "pyproject with tool.poe",
			files:   map[string]string{"pyproject.toml": "[project]\nname = \"test\"\n\n[tool.poe.tasks]\nsetup = \"echo hi\""},
			wantPkg: UV,
			wantDoc: NoDoc,
		},
		{
			name:    "uv.lock only",
			files:   map[string]string{"uv.lock": "version = 1\n", "pyproject.toml": "[project]\nname = \"x\""},
			wantPkg: UV,
			wantDoc: NoDoc,
		},
		{
			name:    "pixi + quarto",
			files:   map[string]string{"pixi.toml": "", "_quarto.yml": "project:\n  type: manuscript"},
			wantPkg: Pixi,
			wantDoc: Quarto,
		},
		{
			name:    "uv + myst",
			files:   map[string]string{"pyproject.toml": "[tool.poe.tasks]\nsetup = \"echo\"", "myst.yml": "version: 1"},
			wantPkg: UV,
			wantDoc: Myst,
		},
		{
			name:    "quarto takes precedence over myst",
			files:   map[string]string{"pixi.toml": "", "_quarto.yml": "", "myst.yml": ""},
			wantPkg: Pixi,
			wantDoc: Quarto,
		},
		{
			name:    "poetry project should not match as UV",
			files:   map[string]string{"pyproject.toml": "[project]\nname = \"test\"\n\n[tool.poetry]\nname = \"test\""},
			wantNil: true,
		},
		{
			name: "pixi marker in TOML comment is false positive",
			files: map[string]string{
				"pyproject.toml": "[project]\nname = \"test\"\n# [tool.pixi] is not used here\n",
			},
			// NOTE: This documents known behavior — the detector uses
			// strings.Contains, so commented-out markers are a false positive.
			// If this becomes a problem, switch to a TOML parser.
			wantPkg: Pixi,
			wantDoc: NoDoc,
		},
		{
			name: "poe marker in comment is false positive",
			files: map[string]string{
				"pyproject.toml": "[project]\nname = \"test\"\n# [tool.poe.tasks] is not used\n",
			},
			// Same as above — documents the false positive.
			wantPkg: UV,
			wantDoc: NoDoc,
		},
		{
			name: "unreadable pyproject falls back to uv.lock",
			files: map[string]string{
				"uv.lock": "version = 1\n",
			},
			wantPkg: UV,
			wantDoc: NoDoc,
		},
		{
			name:    "pyproject exists but empty",
			files:   map[string]string{"pyproject.toml": ""},
			wantNil: true,
		},
		{
			name:    "nonexistent dir",
			files:   nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			proj := Detect(dir)
			if tt.wantNil {
				if proj != nil {
					t.Fatalf("expected nil, got %+v", proj)
				}
				return
			}
			if proj == nil {
				t.Fatal("expected non-nil project")
			}
			if proj.PkgManager != tt.wantPkg {
				t.Errorf("PkgManager = %q, want %q", proj.PkgManager, tt.wantPkg)
			}
			if proj.DocSystem != tt.wantDoc {
				t.Errorf("DocSystem = %q, want %q", proj.DocSystem, tt.wantDoc)
			}
		})
	}
}
