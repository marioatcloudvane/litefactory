package graphexec_test

import (
	"encoding/json"
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

func TestGraphShippedDOTScope(t *testing.T) {
	// The current distribution path has one author: canonical release metadata.
	// Admit only its six existing workflows, never a general graph namespace.
	raw, err := os.ReadFile("../../teams/source/go-development/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var release struct {
		Download string `json:"download"`
	}
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	if !fs.ValidPath(release.Download) || !strings.HasPrefix(release.Download, "go-development/") {
		t.Fatal("unsafe current release path")
	}
	// One frozen test fixture: the pre-worker-sync Implement graph that saved
	// instances may still run. It is test data, never a release or a source.
	allowed := map[string]bool{"../../internal/turn/testdata/implement-v1.0.2/implement.dot": true}
	workflows := []string{"research", "plan", "implement", "validate", "spike", "bug"}
	for _, workflow := range workflows {
		allowed["../../teams/"+release.Download+"/"+workflow+"/"+workflow+".dot"] = true
	}
	// Every release the catalogue ships carries the same six workflows, and each
	// stack team keeps its editable source in its own directory. Only these
	// exact paths are admitted; no general graph namespace.
	rawCatalogue, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalogue struct {
		Releases []struct {
			Slug     string `json:"slug"`
			Download string `json:"download"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(rawCatalogue, &catalogue); err != nil {
		t.Fatal(err)
	}
	for _, r := range catalogue.Releases {
		if !fs.ValidPath(r.Download) || !fs.ValidPath(r.Slug) || strings.Contains(r.Slug, "/") {
			t.Fatalf("unsafe catalogue release %q %q", r.Slug, r.Download)
		}
		for _, workflow := range workflows {
			allowed["../../teams/"+r.Download+"/"+workflow+"/"+workflow+".dot"] = true
			if r.Slug != "go-development" {
				allowed["../../teams/source/"+r.Slug+"/"+workflow+"/"+workflow+".dot"] = true
			}
		}
	}
	// The shared base-team holds the same six graphs with a {{team}} placeholder.
	for _, workflow := range workflows {
		allowed["../../teams/source/base-team/"+workflow+"/"+workflow+".dot"] = true
	}
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
