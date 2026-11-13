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
			if path == "github.com/cloudvane/litefactory/internal/agent" || path == "github.com/cloudvane/litefactory/internal/config" || path == "github.com/cloudvane/litefactory/internal/planbrief" {
				continue
			}
			if path == "github.com/cloudvane/litefactory/pkg/execution/graph" || path == "github.com/cloudvane/litefactory/internal/denypolicy" {
				continue
			}
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") || path == "os" || strings.HasPrefix(path, "os/") || path == "net/http" || strings.HasPrefix(path, "net/http/") || path == "time" {
				t.Errorf("%s imports forbidden %q", name, path)
			}
		}
	}
}

func TestGraphShippedDOTScope(t *testing.T) {
	// Graphs live only in teams/source: the shared base-team holds the seven
	// workflow graphs with a {{team}} placeholder, and a team folder may replace
	// one of its own. Teams are installed from there; no generated release
	// folders exist. Only these exact paths are admitted.
	// One frozen test fixture: the pre-worker-sync Implement graph that saved
	// instances may still run. It is test data, never a release or a source.
	allowed := map[string]bool{"../../internal/turn/testdata/implement-v1.0.2/implement.dot": true}
	workflows := []string{"research", "plan", "implement", "validate", "spike", "doctor", "bug"}
	teams, err := os.ReadDir("../../teams/source")
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range teams {
		if !team.IsDir() || !fs.ValidPath(team.Name()) {
			continue
		}
		for _, workflow := range workflows {
			allowed["../../teams/source/"+team.Name()+"/"+workflow+"/"+workflow+".dot"] = true
		}
	}
	// The factory library holds the Doctor graph, which no team carries.
	allowed["../../teams/source/factory/doctor/doctor.dot"] = true
	err = filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
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
		if strings.HasSuffix(path, ".dot") && !allowed[filepath.ToSlash(path)] {
			t.Errorf("unexpected shipped DOT file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
