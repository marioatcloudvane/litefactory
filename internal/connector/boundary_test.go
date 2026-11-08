package connector

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRemoteConsumerCannotAcquireFactoryStoreOrExecution(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := map[string]string{}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(path, "/")
			name := parts[len(parts)-1]
			if imported.Name != nil {
				name = imported.Name.Name
			}
			imports[name] = path
			for _, forbidden := range []string{"database/sql", "/internal/submission", "/internal/turn", "/internal/daemon", "/pkg/execution"} {
				if strings.Contains(path, forbidden) {
					t.Errorf("%s imports privileged capability %s", entry.Name(), path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			path := imports[pkg.Name]
			if strings.HasSuffix(path, "/internal/store") && !strings.HasPrefix(selector.Sel.Name, "Err") {
				t.Errorf("%s accesses factory persistence symbol %s", entry.Name(), selector.Sel.Name)
			}
			if selector.Sel.Name == "Service" && (strings.HasSuffix(path, "/internal/owneraction") || strings.HasSuffix(path, "/internal/durableworkflow") || strings.HasSuffix(path, "/internal/taskdiscussion")) {
				t.Errorf("%s exposes a concrete factory coordinator", entry.Name())
			}
			return true
		})
	}
}
