package identity_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// identityPath is this package's import path.
const identityPath = "github.com/cloudvane/litefactory/internal/identity"

// identityDir is this package's directory, relative to the module root.
const identityDir = "internal/identity"

// ghLiteralExceptions are the files outside this package that may contain the
// literal "gh" in shipped code, with the reason they may.
//
//   - internal/store/identities.go declares identities.source = 'gh', which is
//     a schema value recording where an identity came from. It is a row, not an
//     invocation.
var ghLiteralExceptions = map[string]string{
	"internal/store/identities.go": "the identities.source column value",
}

// identityImporters are the files that may import this package. `gh` is
// identity discovery only and is reached by `litefactory init` and nothing
// else, so the list is one file long.
//
// The daemon deliberately is not on it: it compares the *stored* owner id with
// the bot it resolves from the API (S-5), and re-deriving the owner from gh at
// daemon start would put an interactive keychain prompt on the launchd path.
var identityImporters = []string{
	"internal/cli/cmd_init.go",
}

// TestGHIsInvokedOnlyFromThisPackage enforces the review invariant of LF-0:
// `gh` is invoked only from internal/identity, and reached only by init.
//
// It is a test rather than a prose rule because the failure it prevents is
// invisible in review. `gh` is authenticated as Mario. Any other package that
// reached for it would be asking Mario's credential a question about the bot,
// which is precisely the identity confusion the bot != owner check exists to
// catch — and it would answer plausibly.
func TestGHIsInvokedOnlyFromThisPackage(t *testing.T) {
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

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, identityDir+"/") {
			return nil
		}

		// A file that does not parse is skipped rather than failed: this test
		// polices where gh is reachable from, it does not stand in for the
		// compiler.
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			//nolint:nilerr // Deliberate: an unparseable file is skipped, not
			// walked out on. Returning parseErr would abort the walk and turn a
			// syntax error anywhere in the module into a failure of this gh-exec
			// check, which the compiler already reports better.
			return nil
		}
		checkImports(t, rel, file)
		checkGHLiterals(t, rel, file)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// checkImports fails when a file that has no business deriving an identity
// imports the package that can.
func checkImports(t *testing.T, rel string, file *ast.File) {
	t.Helper()

	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, `"`) != identityPath {
			continue
		}
		if !slices.Contains(identityImporters, rel) {
			t.Errorf("%s imports %s. That package execs gh, which is authenticated as the owner; "+
				"only %s may reach it. Read the stored identity from internal/store instead.",
				rel, identityPath, strings.Join(identityImporters, ", "))
		}
	}
}

// checkGHLiterals fails on a "gh" string literal outside this package, which is
// how a second exec site would have to start.
func checkGHLiterals(t *testing.T, rel string, file *ast.File) {
	t.Helper()

	if _, allowed := ghLiteralExceptions[rel]; allowed {
		return
	}
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && lit.Value == `"gh"` {
			t.Errorf(`%s contains the literal "gh". gh is invoked only from %s, and only by init.`,
				rel, identityPath)
		}
		return true
	})
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
