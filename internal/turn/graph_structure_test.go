package turn_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Later mode admission must deliberately replace this fence when production
// dispatch is authorized. Function-value references are wiring as well as calls.
func TestExecuteNodeActionHasNoProductionCaller(t *testing.T) {
	t.Parallel()
	declarations := 0
	for _, root := range []string{"../", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "ExecuteNodeAction" {
					declarations++
					ast.Inspect(fn.Body, func(body ast.Node) bool {
						if id, ok := body.(*ast.Ident); ok && id.Name == "ExecuteNodeAction" {
							t.Errorf("%s: recursive dispatch", positions.Position(id.Pos()))
						}
						return true
					})
					return false
				}
				if id, ok := n.(*ast.Ident); ok && id.Name == "ExecuteNodeAction" {
					t.Errorf("%s: production dispatch reference", positions.Position(id.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if declarations != 1 {
		t.Fatalf("found %d dispatch declarations, want 1", declarations)
	}
}

func TestGraphActivitiesHaveNoClockOrRetryLoop(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("graph_*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		timeNames := map[string]bool{}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if name == "time" {
				alias := "time"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				if alias == "." {
					t.Errorf("%s: dot time import obscures clock operations", path)
				}
				timeNames[alias] = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt, *ast.SelectStmt, *ast.SendStmt:
				t.Errorf("%s: asynchronous or blocking dispatch", positions.Position(n.Pos()))
			case *ast.UnaryExpr:
				if node.Op == token.ARROW {
					t.Errorf("%s: channel wait", positions.Position(node.Pos()))
				}
			case *ast.ForStmt:
				t.Errorf("%s: unbounded/retry loop in graph dispatch", positions.Position(node.Pos()))
			case *ast.RangeStmt:
				// Ranges may clone boundary values, but must not dispatch or reschedule.
				ast.Inspect(node.Body, func(body ast.Node) bool {
					if call, ok := body.(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Execute" || sel.Sel.Name == "ExecuteNodeAction") {
							t.Errorf("%s: repeated dispatch", positions.Position(call.Pos()))
						}
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "ExecuteNodeAction" {
							t.Errorf("%s: repeated dispatch", positions.Position(call.Pos()))
						}
					}
					return true
				})
			case *ast.SelectorExpr:
				if id, ok := node.X.(*ast.Ident); ok && timeNames[id.Name] && node.Sel.Name != "Time" {
					t.Errorf("%s: time operation %s in bounded dispatch", positions.Position(node.Pos()), node.Sel.Name)
				}
			}
			return true
		})
	}
	if checked < 2 {
		t.Fatalf("checked %d graph files, want activity and node implementations", checked)
	}
}
