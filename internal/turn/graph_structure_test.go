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

// Research and Plan enter the shared driver. The pinned Bug child and the
// factory-selected Bug parent runtime dispatch their own retained node identities.
// BugRuntime binds package pins, role permissions and domain recovery before
// this call; only its exact production method is an additional approved entry.
// Reject other wiring, including function-value references and nested callbacks.
func TestExecuteNodeActionHasOnlyApprovedProductionCallers(t *testing.T) {
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
				planEntry := filepath.Clean(path) == filepath.Join("..", "turn", "plan_graph.go") && fn.Name.Name == "admitPlanExecution"
				bugChildEntry := false
				if filepath.Clean(path) == filepath.Join("..", "turn", "bug_test_graph.go") && fn.Name.Name == "Advance" && fn.Recv != nil && len(fn.Recv.List) == 1 {
					if pointer, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
						name, ok := pointer.X.(*ast.Ident)
						bugChildEntry = ok && name.Name == "BugTestChild"
					}
				}
				bugParentEntry := false
				if filepath.Clean(path) == filepath.Join("..", "turn", "bug_runtime.go") && fn.Name.Name == "Execute" && fn.Recv != nil && len(fn.Recv.List) == 1 {
					if pointer, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
						name, ok := pointer.X.(*ast.Ident)
						bugParentEntry = ok && name.Name == "BugRuntime"
					}
				}
				if !bugParentEntry && !bugChildEntry && ((!planEntry && filepath.Clean(path) != filepath.Join("..", "turn", "research_workflow.go")) || fn.Recv != nil) {
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
					if !ok || (owners[id.Name] != fn.Name.Name && (!planEntry || id.Name != "ExecuteWorkflow") && ((!bugChildEntry && !bugParentEntry) || id.Name != "ExecuteNodeAction")) {
						return true
					}
					allowed[id] = true
					calls[id.Name]++
					if planEntry {
						assertPlanDispatchPolicy(t, positions, fn, call)
					} else if id.Name == "ExecuteWorkflow" {
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
		want := 2
		if name == "ExecuteNodeAction" {
			want = 3
		}
		if calls[name] != want {
			t.Errorf("found %d direct %s calls from approved %s entries, want %d", calls[name], name, owner, want)
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

func assertPlanDispatchPolicy(t *testing.T, positions *token.FileSet, fn *ast.FuncDecl, call *ast.CallExpr) {
	t.Helper()
	if len(call.Args) == 0 {
		t.Errorf("%s: Plan dispatch lacks policy", positions.Position(call.Pos()))
		return
	}
	arg, ok := call.Args[len(call.Args)-1].(*ast.Ident)
	if !ok || arg.Name != "policy" {
		t.Errorf("%s: Plan dispatch must use local policy", positions.Position(call.Pos()))
		return
	}
	assignments := 0
	for _, stmt := range fn.Body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name != "policy" {
			continue
		}
		assignments++
		value, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || len(value.Args) != 0 {
			t.Error("Plan policy must be factory runtime policy")
			continue
		}
		method, ok := value.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "policy" {
			t.Error("Plan policy must call runtime policy")
			continue
		}
		receiver, ok := method.X.(*ast.Ident)
		if !ok || receiver.Name != "r" {
			t.Error("Plan policy must belong to admitted runtime")
		}
	}
	if assignments != 1 {
		t.Errorf("Plan policy assignment count=%d, want 1", assignments)
	}
}
