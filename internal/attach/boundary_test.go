package attach

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestPackageStaysALeaf enforces US-3's architecture note: internal/attach
// imports internal/config and internal/naming and nothing else of ours.
//
// The reason is that this package is where third-party bytes first enter the
// process. It fetches what a stranger attached to an issue, decides what the
// bytes are, and writes them to disk. Everything it can reach is inside the
// blast radius of getting that wrong, so the set of things it can reach is
// kept to the two packages that only describe where files go.
//
// The forbidden four are named individually rather than caught by a
// deny-everything rule so that the error message says which boundary was
// crossed. internal/daemon and internal/store would make this package part
// of the run loop; internal/harness would let the ingestion vocabulary leak
// into prompt assembly, which is the thing PromptInput.Attachments being a
// plain string exists to prevent; internal/report would invite an attachment
// rejection to be routed through ReplyKind, which the package doc explains
// at length is the wrong home for it.
func TestPackageStaysALeaf(t *testing.T) {
	t.Parallel()

	const ourPrefix = "github.com/cloudvane/litefactory/"
	allowed := map[string]bool{
		ourPrefix + "internal/config": true,
		ourPrefix + "internal/naming": true,
	}

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
			if !strings.HasPrefix(path, ourPrefix) {
				continue // stdlib and third-party are governed elsewhere
			}
			if !allowed[path] {
				t.Errorf("%s imports %s; internal/attach must stay a leaf over internal/config and internal/naming only", name, path)
			}
		}
	}
}

// TestNoForgeClientImport is the sharper half of the boundary above, kept
// separate because it is the one that would be easiest to cross by accident
// and the most expensive to have crossed.
//
// [Fetcher] exists so this package can consume *github.Client without
// naming it. Importing internal/github directly would work, compile, and
// pass every other test — and it would put a package that holds the
// installation token inside the import graph of the package that parses
// stranger-supplied markdown.
func TestNoForgeClientImport(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			if strings.Contains(spec.Path.Value, "internal/github") {
				t.Errorf("%s imports internal/github; consume it through the Fetcher interface instead", name)
			}
		}
	}
}
