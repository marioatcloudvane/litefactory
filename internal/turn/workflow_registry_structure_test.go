package turn_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// A workflow policy reaches the runtime only through RegisteredWorkflowPolicies,
// which the registry-wide guard walks. A production constructor returning a
// WorkflowPolicy that the registry does not call would be a workflow the guard
// never sees, so this refuses it.
func TestEveryWorkflowPolicyConstructorIsRegistered(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	constructors := map[string]token.Position{}
	registered := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
				continue
			}
			if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && id.Name == "WorkflowPolicy" && fn.Name.Name != "RegisteredWorkflowPolicies" {
				constructors[fn.Name.Name] = positions.Position(fn.Pos())
			}
			if fn.Name.Name != "RegisteredWorkflowPolicies" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok {
						registered[id.Name] = true
					}
				}
				return true
			})
		}
	}
	if len(constructors) < 2 {
		t.Fatalf("found %d policy constructors; the scan lost the shipped ones", len(constructors))
	}
	for name, position := range constructors {
		if !registered[name] {
			t.Errorf("%s: %s builds a WorkflowPolicy that RegisteredWorkflowPolicies does not register", position, name)
		}
	}
}
