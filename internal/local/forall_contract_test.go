package local

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/samber/lo"
)

// singleLibraryExempt are the *DB methods allowed to touch d.libraryID without
// calling requireSingleLibrary, because each already handles the merged case
// itself: LibraryID reports 0 for a multi-library handle, libIn widens to an
// IN clause, and init is what populates the fields in the first place.
var singleLibraryExempt = []string{"LibraryID", "libIn", "init"}

// TestSingleLibraryQueriesAreGuarded is the enforcement half of the ForAll
// contract, and the reason the contract is worth stating at all.
//
// A method that binds d.libraryID answers for ONE library. Called on a merged
// (ForAll) handle it does not fail — it quietly answers personal-only, which
// looks exactly like a correct short answer. Nothing in the type system
// distinguishes the two handles, so the only durable guard is a rule that a
// test can check: bind d.libraryID, and you must first call
// requireSingleLibrary.
//
// This walks the package rather than listing methods, so a new one written
// tomorrow is covered without anyone remembering to add it here.
func TestSingleLibraryQueriesAreGuarded(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	sources := lo.FilterMap(entries, func(e os.DirEntry, _ int) (string, bool) {
		n := e.Name()
		return n, !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go")
	})
	if len(sources) == 0 {
		t.Fatal("no non-test .go files found — the walk would vacuously pass")
	}

	fset := token.NewFileSet()
	var unguarded []string
	for _, name := range sources {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			if !isDBReceiver(fn) || slices.Contains(singleLibraryExempt, fn.Name.Name) {
				continue
			}
			if bindsLibraryID(fn) && !callsGuard(fn) {
				unguarded = append(unguarded, fn.Name.Name)
			}
		}
	}

	if len(unguarded) > 0 {
		slices.Sort(unguarded)
		t.Errorf("these *DB methods bind d.libraryID without calling requireSingleLibrary,\n"+
			"so on a ForAll handle they silently answer personal-only:\n  %s\n\n"+
			"Either call d.requireSingleLibrary(\"<what it does>\") first, or convert the\n"+
			"query through d.libIn so it answers across every library in the handle.",
			strings.Join(unguarded, "\n  "))
	}
}

// isDBReceiver reports whether fn is a method on *DB (or DB).
func isDBReceiver(fn *ast.FuncDecl) bool {
	if len(fn.Recv.List) != 1 {
		return false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == "DB"
}

// bindsLibraryID reports whether fn's body mentions d.libraryID — the single
// pinned library, as opposed to the libraryIDs slice libIn widens over.
func bindsLibraryID(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "libraryID" {
			return true
		}
		found = true
		return false
	})
	return found
}

// callsGuard reports whether fn's body calls d.requireSingleLibrary.
func callsGuard(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "requireSingleLibrary" {
			return true
		}
		found = true
		return false
	})
	return found
}
