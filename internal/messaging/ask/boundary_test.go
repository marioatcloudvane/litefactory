package ask

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Ask may consume bounded core projections, but must not acquire persistence,
// lifecycle or live-attempt execution authority to answer a question.
func TestAskConsumesOnlyBoundedFactoryViews(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"database/sql", "/internal/store", "/internal/daemon", "/internal/connector", "/internal/submission", "/internal/turn", "/pkg/execution"} {
				if path == forbidden || strings.HasSuffix(path, forbidden) || strings.Contains(path, forbidden+"/") {
					t.Errorf("%s imports privileged capability %s", entry.Name(), path)
				}
			}
		}
	}
}
