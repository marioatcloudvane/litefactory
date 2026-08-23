package gittest

import (
	"fmt"
	"strings"
	"testing"
)

// Snapshot is everything an observer can see about a working tree without
// changing it: what git status reports, what HEAD points at, and which branch
// is checked out. Together those three cover the working tree, the index and
// the ref that would move if something committed.
//
// Snapshot is comparable, so equality is byte-for-byte on all three fields.
type Snapshot struct {
	// Status is the output of "git status --porcelain".
	Status string
	// Head is the output of "git rev-parse HEAD".
	Head string
	// Branch is the output of "git rev-parse --abbrev-ref HEAD".
	Branch string
}

// Capture records the state of the repository at dir. It runs only read-only
// git commands, in the same hermetic environment the fixtures use, so repeated
// captures differ only if something actually changed.
func Capture(tb testing.TB, dir string) Snapshot {
	tb.Helper()
	home := tb.TempDir()
	return Snapshot{
		Status: run(tb, home, dir, "status", "--porcelain"),
		Head:   run(tb, home, dir, "rev-parse", "HEAD"),
		Branch: run(tb, home, dir, "rev-parse", "--abbrev-ref", "HEAD"),
	}
}

// Diff describes how s and other differ, or returns "" when they are identical.
func (s Snapshot) Diff(other Snapshot) string {
	var b strings.Builder
	field := func(name, before, after string) {
		if before == after {
			return
		}
		fmt.Fprintf(&b, "%s:\n  before: %q\n  after:  %q\n", name, before, after)
	}
	field("git status --porcelain", s.Status, other.Status)
	field("git rev-parse HEAD", s.Head, other.Head)
	field("git rev-parse --abbrev-ref HEAD", s.Branch, other.Branch)
	return b.String()
}

// AssertUnchanged captures the state of the repository at dir, runs fn, captures
// it again, and fails the test unless the two are byte-identical.
//
// This is the shared clone-untouched assertion. It is the mechanical half of
// "litefactory never writes inside the repository you registered": the type
// system removes the ability to write there, and this proves that nothing found
// a way around it — including through git's own side effects.
//
// Use it against a repository that has been dirtied with [Repo.Dirty]. Against a
// clean checkout the assertion is trivially satisfiable and worth nothing.
func AssertUnchanged(tb testing.TB, dir string, fn func()) {
	tb.Helper()

	before := Capture(tb, dir)
	fn()
	after := Capture(tb, dir)

	if diff := before.Diff(after); diff != "" {
		tb.Fatalf("repository at %s was modified:\n%s", dir, diff)
	}
}
