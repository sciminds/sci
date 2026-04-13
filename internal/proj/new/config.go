package new

// config.go — re-renders managed config files (.vscode, .zed) in existing
// projects. [PlanConfig] computes diffs without writing; [Sync] applies them.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/proj"
	"github.com/sciminds/cli/internal/uikit"
)

// ManagedFiles are the config files that proj config manages.
var ManagedFiles = []string{
	".vscode/extensions.json.tmpl",
	".vscode/settings.json.tmpl",
	".zed/settings.json.tmpl",
	".zed/tasks.json.tmpl",
}

// SyncResult holds the output of a sync operation.
type SyncResult struct {
	Dir     string       `json:"dir"`
	Changed []SyncChange `json:"changed"`
	DryRun  bool         `json:"dryRun"`
}

// SyncChange represents a single file change.
type SyncChange struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	Exists  bool   `json:"exists"`
}

// JSON implements cmdutil.Result.
func (r SyncResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r SyncResult) Human() string {
	var b strings.Builder
	changed := lo.CountBy(r.Changed, func(c SyncChange) bool {
		return c.Changed
	})
	for _, c := range r.Changed {
		if c.Changed {
			fmt.Fprintf(&b, "    %s %s\n", uikit.SymOK, c.Path)
		}
	}
	if changed == 0 {
		fmt.Fprintf(&b, "  %s All managed files are up to date.\n", uikit.SymOK)
	} else {
		sym := lo.Ternary(r.DryRun, uikit.SymWarn, uikit.SymOK)
		verb := lo.Ternary(r.DryRun, "would be updated", "updated")
		fmt.Fprintf(&b, "  %s %d file(s) %s.\n", sym, changed, verb)
	}
	return b.String()
}

// ConfigFile describes a managed config file and its rendered content.
type ConfigFile struct {
	Path     string // output path relative to project dir (no .tmpl)
	Content  string // rendered template content
	Exists   bool   // true if file already exists on disk
	Changed  bool   // true if file differs from rendered content
	TmplName string // template name for reference
}

// PlanConfig detects the project and computes what each managed file would
// look like without writing anything. Returns the list of applicable files.
func PlanConfig(dir string) ([]ConfigFile, error) {
	p := proj.Detect(dir)
	if p == nil {
		return nil, fmt.Errorf("no Python project detected in %s", dir)
	}

	vars := TemplateVars{
		PkgManager: string(p.PkgManager),
		DocSystem:  string(p.DocSystem),
	}

	var files []ConfigFile
	for _, tmplName := range ManagedFiles {
		outName := strings.TrimSuffix(tmplName, ".tmpl")
		rendered, err := RenderFile(tmplName, vars)
		if err != nil {
			return nil, fmt.Errorf("rendering %s: %w", tmplName, err)
		}
		if strings.TrimSpace(rendered) == "" {
			continue
		}

		outPath := filepath.Join(dir, outName)
		existing, err := os.ReadFile(outPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", outName, err)
		}
		exists := err == nil

		files = append(files, ConfigFile{
			Path:     outName,
			Content:  rendered,
			Exists:   exists,
			Changed:  string(existing) != rendered,
			TmplName: tmplName,
		})
	}

	return files, nil
}

// ApplyConfigFiles writes the selected config files to the project directory.
func ApplyConfigFiles(dir string, files []ConfigFile) error {
	for _, f := range files {
		outPath := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Sync re-renders managed config files in an existing project directory.
func Sync(dir string, dryRun bool) (*SyncResult, error) {
	files, err := PlanConfig(dir)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{
		Dir:    dir,
		DryRun: dryRun,
		Changed: lo.Map(files, func(f ConfigFile, _ int) SyncChange {
			return SyncChange{Path: f.Path, Changed: f.Changed, Exists: f.Exists}
		}),
	}

	if !dryRun {
		for _, f := range files {
			if f.Changed {
				if err := ApplyConfigFiles(dir, []ConfigFile{f}); err != nil {
					return nil, err
				}
			}
		}
	}

	return result, nil
}
