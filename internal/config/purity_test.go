package config

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

// moduleRoot resolves the repository root from this package's directory. Go
// runs a test with its working directory set to the package directory, so the
// relative hop is stable.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod not found at %s: %v", root, err)
	}
	return root
}

// TestValidationHasNoThirdPartyImports enforces a review invariant that would
// otherwise rot: everything in this package except the YAML decode itself is
// stdlib-only.
//
// The reason is blast radius. Validation is what stands between a hand-edited
// file and the daemon starting, and the secret scan is what keeps credentials
// out of config.yaml. Neither should be able to change behaviour because a
// dependency was upgraded.
func TestValidationHasNoThirdPartyImports(t *testing.T) {
	// The one file allowed to import a third-party package, and the only
	// package it may import.
	const decodeFile = "config.go"
	const decodePackage = "gopkg.in/yaml.v3"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
			}
			if !isThirdParty(path) {
				continue
			}
			if name == decodeFile && path == decodePackage {
				continue
			}
			t.Errorf("%s imports %q; only %s may import %q, and only for decoding",
				name, path, decodeFile, decodePackage)
		}
	}
}

// isThirdParty reports whether an import path leaves the standard library. A
// standard-library path has no dot in its first segment.
func isThirdParty(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return strings.Contains(first, ".")
}

// TestForbiddenFlagIsAbsentFromTheTree is the executable form of the review
// invariant "claude's permission-skipping flag is never constructed anywhere
// in the tree" (see config.forbiddenFlag for the flag itself). A prose rule in
// a plan does not survive the evening someone spends fighting a blocked tool
// call; this fails the build instead.
//
// The needle is assembled from fragments so that this file is not itself the
// match, which is also why config.forbiddenFlag builds it the same way.
func TestForbiddenFlagIsAbsentFromTheTree(t *testing.T) {
	needle := forbiddenFlag()
	root := moduleRoot(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "vendor", ".aiteam", ".claude":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), needle) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			t.Errorf("%s contains the permission-skipping flag; litefactory never constructs it", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
