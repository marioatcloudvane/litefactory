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

// Research is the only adopted mode. Its policy wrapper is the only production
// entry to the shared driver, which owns the only activity dispatch call.
// Reject other wiring, including function-value references and nested callbacks.
func TestExecuteNodeActionHasOnlyResearchProductionCaller(t *testing.T) {
	t.Parallel()
	declarations := map[string]int{}
	calls := map[string]int{}
	owners := map[string]string{
		"ExecuteNodeAction": "ExecuteWorkflow",
		"ExecuteWorkflow":   "ExecuteResearchWorkflow",
	}
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
			allowed := map[*ast.Ident]bool{}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, tracked := owners[fn.Name.Name]; tracked {
					declarations[fn.Name.Name]++
					allowed[fn.Name] = true
				}
				if filepath.Clean(path) != filepath.Join("..", "turn", "research_workflow.go") || fn.Recv != nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if _, nested := n.(*ast.FuncLit); nested {
						return false
					}
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					id, ok := call.Fun.(*ast.Ident)
					if !ok || owners[id.Name] != fn.Name.Name {
						return true
					}
					allowed[id] = true
					calls[id.Name]++
					if id.Name == "ExecuteWorkflow" {
						if len(call.Args) == 0 {
							t.Errorf("%s: shared workflow lacks Research policy", positions.Position(call.Pos()))
							return true
						}
						policy, ok := call.Args[len(call.Args)-1].(*ast.CallExpr)
						if !ok {
							t.Errorf("%s: shared workflow requires explicit Research policy", positions.Position(call.Pos()))
							return true
						}
						policyName, ok := policy.Fun.(*ast.Ident)
						if !ok || policyName.Name != "ResearchWorkflowPolicy" || len(policy.Args) != 0 {
							t.Errorf("%s: production workflow must use ResearchWorkflowPolicy()", positions.Position(policy.Pos()))
						}
					}
					return true
				})
			}
			ast.Inspect(file, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if _, tracked := owners[id.Name]; tracked && !allowed[id] {
					t.Errorf("%s: unauthorized production dispatch reference %s", positions.Position(id.Pos()), id.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, owner := range owners {
		if calls[name] != 1 {
			t.Errorf("found %d direct %s calls from %s, want 1", calls[name], name, owner)
		}
		if declarations[name] != 1 {
			t.Errorf("found %d %s declarations, want 1", declarations[name], name)
		}
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
