package new

// templates.go — embedded template filesystem and rendering engine. Templates
// live in templates/python/ and are rendered with [text/template]. Files that
// render to all-whitespace are silently skipped (conditional on TemplateVars).

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:templates/python
var templateFS embed.FS

// TemplateVars holds the variables available to all project templates.
type TemplateVars struct {
	ProjectName string
	PkgManager  string // "pixi" or "uv"
	DocSystem   string // "quarto", "myst", or "none"
	AuthorName  string
	AuthorEmail string
	Description string
}

// RenderAll walks the embedded template filesystem, renders .tmpl files with
// the given vars, copies non-.tmpl files verbatim, and writes results to dest.
// Files that render to all-whitespace are silently skipped (conditional files).
// Returns the list of relative paths that were written.
func RenderAll(vars TemplateVars, dest string) ([]string, error) {
	var created []string
	root := "templates/python"

	err := fs.WalkDir(templateFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}

		var outPath string
		var content []byte

		if strings.HasSuffix(relPath, ".tmpl") {
			// Render template
			outPath = strings.TrimSuffix(relPath, ".tmpl")
			rendered, err := renderTemplate(string(data), vars, relPath)
			if err != nil {
				return err
			}
			// Skip files that render to all whitespace (conditional files)
			if strings.TrimSpace(rendered) == "" {
				return nil
			}
			content = []byte(rendered)
		} else {
			// Copy verbatim
			outPath = relPath
			content = data
		}

		fullPath := filepath.Join(dest, outPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			return err
		}
		created = append(created, outPath)
		return nil
	})

	return created, err
}

// RenderFile renders a single template by name and returns the result.
// The name is relative to templates/python/ (e.g. ".vscode/settings.json.tmpl").
func RenderFile(name string, vars TemplateVars) (string, error) {
	path := filepath.Join("templates/python", name)
	data, err := templateFS.ReadFile(path)
	if err != nil {
		return "", err
	}
	return renderTemplate(string(data), vars, name)
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
