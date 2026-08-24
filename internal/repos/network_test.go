package repos_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestRegistrationIsOffline makes the "`repo add` makes no network call"
// review invariant mechanical.
//
// The invariant is easy to state and easy to break: the obvious way to find a
// remote's default branch is "git ls-remote --symref origin HEAD", and it
// would pass every other test in this package on a developer's machine while
// making `repo add` fail on a plane, behind a captive portal, or with a locked
// SSH agent. There is nothing about that failure that a reader of the diff
// would notice, so it is checked here instead of hoped for in review.
//
// Two things are banned. ls-remote, which reaches the remote directly, and
// Clone.Fetch, which is the only method on the type that talks to the network.
// Everything else [git.Clone] exposes is local plumbing.
//
// Comments are stripped before the scan, so the package may — and does —
// explain in prose exactly which calls it is refusing to make.
func TestRegistrationIsOffline(t *testing.T) {
	t.Parallel()

	banned := []struct {
		needle string
		why    string
	}{
		{"ls-remote", "ls-remote contacts the remote; read the cached refs instead"},
		{".Fetch(", "Clone.Fetch is the one networked method on the type; registration must not call it"},
		{"http://", "registration makes no HTTP call"},
		{"https://", "registration makes no HTTP call"},
	}

	fileSet := token.NewFileSet()
	// Package-level parse with the default mode: comments are not attached, so
	// printing the AST back yields the code alone.
	//nolint:staticcheck // SA1019: ParseDir is deprecated in favour of
	// golang.org/x/tools/go/packages, which is a third-party dependency. This
	// module ships three, deliberately, and adding a fourth to the graph so an
	// invariant test can parse its own directory is a worse trade than using a
	// deprecated stdlib function. The deprecation is about build tags, and no
	// file in this package is behind one.
	pkgs, err := parser.ParseDir(fileSet, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			code := printFile(t, fileSet, file)
			for _, ban := range banned {
				if strings.Contains(code, ban.needle) {
					t.Errorf("%s contains %q: %s", name, ban.needle, ban.why)
				}
			}
		}
	}
}

// printFile renders a parsed file back to source, without its comments.
func printFile(t *testing.T, fileSet *token.FileSet, file *ast.File) string {
	t.Helper()

	var b strings.Builder
	if err := (&printer.Config{Mode: printer.RawFormat}).Fprint(&b, fileSet, file); err != nil {
		t.Fatalf("print file: %v", err)
	}
	return b.String()
}
