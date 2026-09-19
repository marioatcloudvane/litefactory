package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// These fences are deliberately removed only when later mode work authorizes
// production admission. A function-value reference is wiring too, not just a call.
func TestGraphPollHasNoProductionCallerIncludingTick(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	foundPoll, foundTick := false, false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if fn.Name.Name == "pollDueGraphInstances" {
					foundPoll = true
					continue
				}
				if fn.Name.Name == "nextGraphAttemptAt" {
					continue
				}
				if fn.Name.Name == "Tick" {
					foundTick = true
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && (id.Name == "pollDueGraphInstances" || id.Name == "nextGraphAttemptAt") {
					t.Errorf("%s: production reference to unwired %s", fset.Position(id.Pos()), id.Name)
				}
				return true
			})
		}
	}
	if !foundPoll || !foundTick {
		t.Fatalf("structural guard did not find poller/Tick: %v/%v", foundPoll, foundTick)
	}
}

func TestGraphHelpersPureAndNonblocking(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"graph_poll.go", "graph_backoff.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			allowed := path == "time"
			if name == "graph_poll.go" {
				allowed = allowed || path == "context" || path == "fmt" || path == "github.com/cloudvane/litefactory/internal/store"
			}
			if !allowed {
				t.Errorf("%s: forbidden dependency %q", name, path)
			}
			if spec.Name != nil {
				t.Errorf("%s: import aliases require updating the structural guard", name)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt, *ast.SendStmt, *ast.SelectStmt, *ast.ChanType:
				t.Errorf("%s: asynchronous or blocking operation", fset.Position(n.Pos()))
			case *ast.UnaryExpr:
				if node.Op == token.ARROW {
					t.Errorf("%s: channel receive", fset.Position(node.Pos()))
				}
			case *ast.SelectorExpr:
				if node.Sel.Name == "CancelFunc" {
					t.Errorf("%s: cancellation ownership", fset.Position(node.Pos()))
				}
			case *ast.CallExpr:
				// An explicit call inventory prevents introducing clock reads, dispatch,
				// mutation, sleeps or opaque local wrappers that hide any of those effects.
				selector, ok := node.Fun.(*ast.SelectorExpr)
				allowed := false
				if ok {
					receiver, named := selector.X.(*ast.Ident)
					if named {
						call := receiver.Name + "." + selector.Sel.Name
						if name == "graph_poll.go" {
							allowed = call == "fmt.Errorf" || call == "db.ListDueGraphInstances"
						}
						if name == "graph_backoff.go" {
							allowed = call == "last.Add" || call == "time.Duration"
						}
					}
				}
				if !allowed {
					t.Errorf("%s: call outside pure helper inventory", fset.Position(node.Pos()))
				}
			}
			return true
		})
	}
}
