// Package main implements gen-reference, which generates
// internal/uikit/REFERENCE.md — a
// categorized quick-reference table of every exported top-level symbol
// in package uikit. Symbols are grouped by category derived from their
// source file prefix (color_*, input_*, layout_*, ui_*, render_md*,
// line_editor*, run_*). Descriptions come from the first line of each
// symbol's godoc comment.
//
// Run via: just docs-uikit
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
)

type category struct {
	name  string
	order int
}

// fileCategory maps a source file name to a category. Order matters:
// longer prefixes win (ui_form → Forms, not UI Components).
var categoryRules = []struct {
	prefix string
	cat    category
}{
	{"ui_form", category{"Forms", 60}},
	{"line_editor", category{"Text Editing", 70}},
	{"render_md", category{"Markdown", 50}},
	{"color_", category{"Colors", 10}},
	{"input_", category{"Input", 20}},
	{"layout_", category{"Layout", 30}},
	{"ui_", category{"Components", 40}},
	{"run_", category{"Runtime", 80}},
}

func categorize(fileName string) (category, bool) {
	base := filepath.Base(fileName)
	for _, r := range categoryRules {
		if strings.HasPrefix(base, r.prefix) {
			return r.cat, true
		}
	}
	return category{}, false
}

type entry struct {
	name string
	kind string // "type", "func", "var", "const"
	desc string
	cat  category
	file string
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen-reference <pkg-dir> <output-file>")
		os.Exit(2)
	}
	pkgDir, outPath := os.Args[1], os.Args[2]

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi os.FileInfo) bool { //nolint:staticcheck // tool script; build tags not relevant
		n := fi.Name()
		return strings.HasSuffix(n, ".go") &&
			!strings.HasSuffix(n, "_test.go") &&
			n != "doc.go"
	}, parser.ParseComments)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	var entries []entry
	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			cat, ok := categorize(fileName)
			if !ok {
				continue
			}
			entries = append(entries, extractEntries(file, cat, fileName)...)
		}
	}

	// Group by category, then sort alphabetically within each.
	grouped := lo.GroupBy(entries, func(e entry) string { return e.cat.name })
	cats := lo.UniqBy(lo.Map(entries, func(e entry, _ int) category { return e.cat }),
		func(c category) string { return c.name })
	slices.SortFunc(cats, func(a, b category) int { return a.order - b.order })

	var out bytes.Buffer
	out.WriteString("# uikit — quick reference\n\n")
	out.WriteString("Auto-generated from godoc comments. Do not edit by hand.\n")
	out.WriteString("Regenerate with `just docs-uikit`.\n\n")
	out.WriteString("Categories follow the file-prefix layout documented in [doc.go](./doc.go).\n")
	out.WriteString("For full signatures run `go doc ./internal/uikit <Symbol>`.\n\n")

	for _, c := range cats {
		rows := grouped[c.name]
		slices.SortFunc(rows, func(a, b entry) int {
			if a.kind != b.kind {
				return kindOrder(a.kind) - kindOrder(b.kind)
			}
			return strings.Compare(a.name, b.name)
		})
		fmt.Fprintf(&out, "## %s\n\n", c.name)
		out.WriteString("| Symbol | Kind | Description |\n")
		out.WriteString("|---|---|---|\n")
		for _, e := range rows {
			fmt.Fprintf(&out, "| `%s` | %s | %s |\n", e.name, e.kind, escapePipes(e.desc))
		}
		out.WriteString("\n")
	}

	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d symbols across %d categories)\n", outPath, len(entries), len(cats))
}

func extractEntries(file *ast.File, cat category, fileName string) []entry {
	var out []entry
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// Skip methods — top-level funcs only.
			if d.Recv != nil {
				continue
			}
			if !d.Name.IsExported() {
				continue
			}
			out = append(out, entry{
				name: d.Name.Name,
				kind: "func",
				desc: firstLine(d.Doc),
				cat:  cat,
				file: fileName,
			})
		case *ast.GenDecl:
			groupDoc := firstLine(d.Doc)
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			// Collect value specs that share the group doc so we can
			// collapse them into a single row.
			var sharedNames []string
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					desc := firstLine(s.Doc)
					if desc == "" {
						desc = groupDoc
					}
					out = append(out, entry{
						name: s.Name.Name,
						kind: typeKind(s),
						desc: desc,
						cat:  cat,
						file: fileName,
					})
				case *ast.ValueSpec:
					exportedNames := lo.Filter(s.Names, func(n *ast.Ident, _ int) bool {
						return n.IsExported()
					})
					if len(exportedNames) == 0 {
						continue
					}
					names := lo.Map(exportedNames, func(n *ast.Ident, _ int) string { return n.Name })
					// If this spec has its own doc, emit a dedicated row.
					// Otherwise collect into shared-group bucket.
					if ownDoc := firstLine(s.Doc); ownDoc != "" {
						out = append(out, entry{
							name: strings.Join(names, ", "),
							kind: kind,
							desc: ownDoc,
							cat:  cat,
							file: fileName,
						})
					} else {
						sharedNames = append(sharedNames, names...)
					}
				}
			}
			if len(sharedNames) > 0 && groupDoc != "" {
				out = append(out, entry{
					name: strings.Join(sharedNames, ", "),
					kind: kind,
					desc: groupDoc,
					cat:  cat,
					file: "",
				})
			}
		}
	}
	return out
}

// kindOrder sorts rows within a category: types first (most important
// for API discovery), then funcs, then constructors' supporting values.
func kindOrder(kind string) int {
	switch kind {
	case "type":
		return 0
	case "interface":
		return 1
	case "func type":
		return 2
	case "func":
		return 3
	case "var":
		return 4
	case "const":
		return 5
	default:
		return 99
	}
}

func typeKind(s *ast.TypeSpec) string {
	switch s.Type.(type) {
	case *ast.InterfaceType:
		return "interface"
	case *ast.StructType:
		return "type"
	case *ast.FuncType:
		return "func type"
	default:
		return "type"
	}
}

func firstLine(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	text := cg.Text()
	return strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
