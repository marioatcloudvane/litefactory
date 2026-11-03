package messaging_test

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

func TestMessagingConsumerCannotAcquireFactoryPersistenceOrWorker(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			parts := strings.Split(imported, "/")
			name := parts[len(parts)-1]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = imported
			if imported == "database/sql" || strings.HasSuffix(imported, "/internal/turn") || strings.HasSuffix(imported, "/internal/daemon") || strings.HasSuffix(imported, "/internal/submission") {
				t.Errorf("%s imports privileged capability %s", path, imported)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selected, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selected.X.(*ast.Ident)
			if !ok {
				return true
			}
			imported := imports[pkg.Name]
			if strings.HasSuffix(imported, "/internal/store") {
				// Error identity and immutable context fences retain compatibility; storage
				// types, database constructors and row operations are not consumer ports.
				name := selected.Sel.Name
				if !strings.HasPrefix(name, "Err") && name != "FormatTime" && name != "WithStandardsConversationOperation" && name != "WithoutStandardsConversationOperation" && name != "InboxTicketIdentity" {
					t.Errorf("%s accesses factory persistence symbol %s", path, name)
				}
			}
			if strings.HasSuffix(imported, "/internal/owneraction") && selected.Sel.Name == "Service" {
				t.Errorf("%s acquires full owner-action coordinator", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
