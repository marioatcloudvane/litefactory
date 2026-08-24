package git

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// constructorFile and constructorFunc are the one place a Worktree may be built.
const (
	constructorFile = "worktree.go"
	constructorFunc = "newWorktree"
)

// TestWorktreeIsBuiltInExactlyOnePlace enforces the claim worktree.go makes
// about itself.
//
// The compile-time half of the guarantee — unexported fields, no exported
// constructor — stops code outside this package from building a Worktree
// pointing at the registered clone. It says nothing about code inside this
// package, and inside is where the overlap check lives. A future method that
// writes its own &Worktree{...} would be a perfectly ordinary-looking edit that
// silently routes around [Clone.newWorktree] and its refusal.
//
// So the funnel is checked mechanically rather than remembered: every composite
// literal of the type must be the one inside newWorktree. Failing here is not a
// style complaint. It means a Worktree exists that was never checked against the
// clone's path, which is the single thing this package exists to prevent.
func TestWorktreeIsBuiltInExactlyOnePlace(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	//nolint:staticcheck // SA1019: ParseDir is deprecated in favour of
	// golang.org/x/tools/go/packages, which is a third-party dependency. This
	// module ships three, deliberately, and adding a fourth to the graph so an
	// invariant test can parse its own directory is a worse trade than using a
	// deprecated stdlib function. The deprecation is about build tags, and no
	// file in this package is behind one.
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the package source: %v", err)
	}

	var sites []string
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			enclosing := ""
			ast.Inspect(file, func(node ast.Node) bool {
				if fn, ok := node.(*ast.FuncDecl); ok {
					enclosing = fn.Name.Name
					return true
				}
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				ident, ok := lit.Type.(*ast.Ident)
				if !ok || ident.Name != "Worktree" {
					return true
				}
				if filepath.Base(name) == constructorFile && enclosing == constructorFunc {
					return true
				}
				sites = append(sites, fset.Position(lit.Pos()).String()+" (in "+enclosing+")")
				return true
			})
		}
	}

	if len(sites) > 0 {
		t.Fatalf("Worktree is built outside %s.%s, at:\n  %s\n\n"+
			"Every Worktree must come from %s.%s, which refuses a path overlapping "+
			"the registered clone. A literal written anywhere else is a handle that "+
			"was never checked against the clone's path.",
			constructorFile, constructorFunc, strings.Join(sites, "\n  "),
			constructorFile, constructorFunc)
	}
}
