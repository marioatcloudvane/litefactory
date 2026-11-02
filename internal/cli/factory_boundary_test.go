package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// CLI composition may open, migrate and close dependencies. Reading or changing
// factory state belongs to the focused application services, including commands
// that operate while the daemon is stopped.
func TestCLIHandlersDoNotAccessFactoryStorage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	databaseNames := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			pointer, ok := field.Type.(*ast.StarExpr)
			if !ok {
				return true
			}
			selector, ok := pointer.X.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "DB" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && pkg.Name == "store" {
				for _, name := range field.Names {
					databaseNames[name.Name] = true
				}
			}
			return true
		})
	}
	// Include existing core-domain composition fields. They supply trusted host
	// dependencies; they must not become a handler's alternate persistence route.
	databaseNames["DB"] = true
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range assignment.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok {
					continue
				}
				factory, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || factory.Sel.Name != "Open" {
					continue
				}
				pkg, ok := factory.X.(*ast.Ident)
				if ok && pkg.Name == "store" && len(assignment.Lhs) > 0 {
					if name, ok := assignment.Lhs[0].(*ast.Ident); ok {
						databaseNames[name.Name] = true
					}
				}
			}
			return true
		})
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			var receiver string
			switch base := selector.X.(type) {
			case *ast.Ident:
				receiver = base.Name
			case *ast.SelectorExpr:
				receiver = base.Sel.Name
			}
			if databaseNames[receiver] && selector.Sel.Name != "Close" && selector.Sel.Name != "Migrate" {
				t.Errorf("%s: factory storage access %s belongs in an owned operation", fset.Position(selector.Pos()), selector.Sel.Name)
			}
			return true
		})
	}
}
