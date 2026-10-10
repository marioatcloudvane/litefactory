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

func TestAdapterImportBoundary(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		adapter := strings.Contains(path, filepath.Join("messaging", "discord"))
		daemon := strings.Contains(path, filepath.Join("internal", "daemon"))
		for _, imported := range source.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(name, "/messaging/discord") && !adapter && !daemon {
				t.Errorf("forbidden adapter import: %s", path)
			}
		}
		if strings.Contains(path, filepath.Join("internal", "messaging")) && !adapter {
			ast.Inspect(source, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), "discord") {
					t.Errorf("adapter identifier in neutral code: %s", id.Name)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
