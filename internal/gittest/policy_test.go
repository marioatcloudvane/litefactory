package gittest_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gittestPath is the import path this package must never appear under in
// shipped code.
const gittestPath = "github.com/cloudvane/litefactory/internal/gittest"

// TestOnlyTestsImportGittest enforces the exception this package is granted.
//
// internal/git is the only package allowed to exec git, and its Clone type has
// no way to commit — that is how "litefactory never writes inside the
// repository you registered" is made structural. Fixtures have to commit, so
// this package execs git directly. That is only safe while nothing in the
// shipped binary can reach it, which is what this test checks: no non-test file
// anywhere in the module may import it.
func TestOnlyTestsImportGittest(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name != "." && (strings.HasPrefix(name, ".") || name == "dist") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Import-only parsing, and a file that does not parse is skipped
		// rather than failed: this test polices imports, it does not stand in
		// for the compiler.
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			//nolint:nilerr // Deliberate: an unparseable file is skipped, not
			// walked out on. Returning parseErr would abort the walk and turn a
			// syntax error anywhere in the module into a failure of this import
			// check, which the compiler already reports better.
			return nil
		}
		for _, spec := range file.Imports {
			if strings.Trim(spec.Path.Value, `"`) == gittestPath {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				t.Errorf("%s imports %s. That package execs git directly and is "+
					"test support only; shipped code must go through internal/git.",
					rel, gittestPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}
