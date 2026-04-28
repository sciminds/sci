package new

// templates.go — embedded template filesystem and rendering engine.
//
// Templates compose by overlay: each project kind/option set picks an ordered
// list of root directories under templates/, walked in order. Later roots win
// on output-path collisions, so per-kind overlays can override the shared
// _paper/ scaffolding without touching it. Files that render to all-whitespace
// are silently skipped (conditional on TemplateVars).

import (
	"bytes"
	"embed"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
)

//go:embed all:templates
var templateFS embed.FS

// TemplateVars holds the variables available to all project templates.
type TemplateVars struct {
	ProjectName string
	Kind        string // "python" (default) or "writing"
	PkgManager  string // "pixi" or "uv" (Python projects only)
	DocSystem   string // "quarto", "myst", or "none" (Python projects only)
	MdLayout    string // "single-file" (default) or "composed" — manuscript layout
	Template    string // "lab" (default), "default", or any MyST template name
	AuthorName  string
	AuthorEmail string
	Description string
}

// applyDefaults fills in the canonical defaults for any unset fields. Mutates
// vars in place. Called by the public Render entry points so callers don't have
// to remember the defaults.
func applyDefaults(vars *TemplateVars) {
	if vars.Kind == "" {
		vars.Kind = "python"
	}
	if vars.MdLayout == "" {
		vars.MdLayout = "single-file"
	}
	if vars.Template == "" {
		vars.Template = "lab"
	}
}

// renderRoots returns the ordered list of embedded template roots to walk for
// the given vars. Later roots overlay earlier on output-path collisions.
//
// Layout:
//   - writing       → _paper, _paper-{single|composed}, [_paper-template-lab], writing
//   - python+myst   → _paper, _paper-{single|composed}, [_paper-template-lab], python
//   - python+other  → python only
func renderRoots(vars TemplateVars) []string {
	switch vars.Kind {
	case "writing":
		return paperRoots(vars, "templates/writing")
	case "python":
		if vars.DocSystem == "myst" {
			return paperRoots(vars, "templates/python")
		}
		return []string{"templates/python"}
	}
	return nil
}

func paperRoots(vars TemplateVars, kindOverlay string) []string {
	roots := []string{"templates/_paper"}
	if vars.MdLayout == "composed" {
		roots = append(roots, "templates/_paper-composed")
	} else {
		roots = append(roots, "templates/_paper-single")
	}
	if vars.Template == "lab" {
		roots = append(roots, "templates/_paper-template-lab")
	}
	roots = append(roots, kindOverlay)
	return roots
}

// fileEntry is the resolved content for one output path after overlay merging.
type fileEntry struct {
	rootIndex int
	rootPath  string // embedded path the file came from (for template name)
	isTmpl    bool
	data      []byte
}

// RenderAll walks the overlay roots in order and writes each unique output
// path to dest. Later roots overlay earlier on path collisions. .tmpl files
// are rendered with [text/template]; non-.tmpl files are copied verbatim.
// Files that render to all-whitespace are silently skipped.
// Returns the sorted list of relative output paths that were written.
func RenderAll(vars TemplateVars, dest string) ([]string, error) {
	applyDefaults(&vars)

	files, err := collectOverlayFiles(vars)
	if err != nil {
		return nil, err
	}

	var created []string
	for _, outPath := range slices.Sorted(maps.Keys(files)) {
		entry := files[outPath]

		var content []byte
		if entry.isTmpl {
			rendered, err := renderTemplate(string(entry.data), vars, entry.rootPath)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(rendered) == "" {
				continue
			}
			content = []byte(rendered)
		} else {
			content = entry.data
		}

		fullPath := filepath.Join(dest, outPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			return nil, err
		}
		created = append(created, outPath)
	}

	return created, nil
}

// collectOverlayFiles walks every root in vars's overlay list and returns the
// effective per-output-path file map. Later roots overwrite earlier on
// collision (output paths compared after stripping .tmpl).
func collectOverlayFiles(vars TemplateVars) (map[string]fileEntry, error) {
	roots := renderRoots(vars)
	out := make(map[string]fileEntry)

	for i, root := range roots {
		err := fs.WalkDir(templateFS, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// A non-existent overlay root is fine — that just means this
				// kind/layout doesn't ship that overlay.
				if os.IsNotExist(err) {
					return fs.SkipDir
				}
				return err
			}
			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			outPath := strings.TrimSuffix(rel, ".tmpl")

			data, err := templateFS.ReadFile(path)
			if err != nil {
				return err
			}

			out[outPath] = fileEntry{
				rootIndex: i,
				rootPath:  path,
				isTmpl:    strings.HasSuffix(rel, ".tmpl"),
				data:      data,
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// RenderFile renders a single template by name and returns the result.
// The name is the output path relative to the project root (with .tmpl
// suffix), e.g. "myst.yml.tmpl" or ".vscode/settings.json.tmpl". The overlay
// list is searched in reverse so the last-defining root wins.
func RenderFile(name string, vars TemplateVars) (string, error) {
	applyDefaults(&vars)

	roots := renderRoots(vars)
	outPath := strings.TrimSuffix(name, ".tmpl")

	for i := len(roots) - 1; i >= 0; i-- {
		// Try as-is first (user passed the .tmpl suffix), then plain.
		for _, candidate := range []string{
			filepath.Join(roots[i], name),
			filepath.Join(roots[i], outPath),
		} {
			data, err := templateFS.ReadFile(candidate)
			if err != nil {
				continue
			}
			return renderTemplate(string(data), vars, candidate)
		}
	}

	return "", fs.ErrNotExist
}

func renderTemplate(tmplText string, vars TemplateVars, name string) (string, error) {
	t, err := template.New(name).Parse(tmplText)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
