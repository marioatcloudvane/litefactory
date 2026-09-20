package graphexec_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These source checks deliberately read repository files: the frozen story
// requires structural evidence in addition to the in-memory behavior tests.
func TestGraphPackagePurity(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "github.com/cloudvane/litefactory/internal/agent" || path == "github.com/cloudvane/litefactory/internal/config" {
				continue
			}
			if name == "dot.go" && strings.HasPrefix(path, "gonum.org/v1/gonum/") {
				continue
			}
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") || path == "os" || strings.HasPrefix(path, "os/") || path == "net/http" || strings.HasPrefix(path, "net/http/") || path == "time" {
				t.Errorf("%s imports forbidden %q", name, path)
			}
		}
	}
}

// shippedDOT is the approved set: one editable workflow per mode. A DOT file
// outside it is either a stray copy or a mode that shipped without the rest of
// its package. plan.dot is retained reference material for a Plan graph that is
// not wired up: nothing loads it, and its lf_plan_* actions are not registered.
var shippedDOT = map[string]bool{
	"../../internal/teamsource/research/research.dot": true,
	"../../internal/teamsource/plan/plan.dot":         true,
}

func TestGraphShippedDOTScope(t *testing.T) {
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".dot") && !shippedDOT[filepath.ToSlash(path)] {
			t.Errorf("unexpected shipped DOT file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
