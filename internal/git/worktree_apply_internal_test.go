package git

import (
	"errors"
	"strings"
	"testing"
)

// TestContainedResolvedPathRefusesAPathNotInTheAllowedList is AC-12.7's
// fourth case, checked directly against containedResolvedPath rather than
// through [Worktree.WriteResolvedFiles] or [Worktree.StageResolvedFiles]:
// both of those exported entry points always call this with a path drawn
// from the very allowed slice they pass alongside it, which makes "the path
// is not one of the confirmed files" unreachable through either of them
// today. The check is still this function's own defence-in-depth (S-13) —
// it never trusts a path just because some other caller already validated
// one, and a future call site that passed an allowed list built separately
// from the paths it iterates must still be refused, not silently accepted
// because a path happened to look otherwise well-formed.
func TestContainedResolvedPathRefusesAPathNotInTheAllowedList(t *testing.T) {
	t.Parallel()

	allowed := []ResolvedFile{{Path: "README.md", Resolution: "confirmed content\n"}}

	_, err := containedResolvedPath(t.TempDir(), "NOTES.md", allowed)
	if !errors.Is(err, ErrPathNotContained) {
		t.Fatalf("error = %v, want ErrPathNotContained", err)
	}
}

// TestContainedResolvedPathAcceptsAPathInTheAllowedList is the positive
// counterpart: a clean, relative path that is both listed and physically
// reachable inside the worktree root resolves to the joined absolute path.
func TestContainedResolvedPathAcceptsAPathInTheAllowedList(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	allowed := []ResolvedFile{{Path: "internal/pkg/file.go", Resolution: "package pkg\n"}}

	got, err := containedResolvedPath(root, "internal/pkg/file.go", allowed)
	if err != nil {
		t.Fatalf("containedResolvedPath: %v", err)
	}
	want := root + "/internal/pkg/file.go"
	if got != want {
		t.Errorf("containedResolvedPath = %q, want %q", got, want)
	}
}

// TestCheckResolvedFilesAggregatesEveryMalformedPath is watch-out territory
// this package documents on [checkResolvedFiles] itself: every entry is
// checked independently and every problem is reported through errors.Join,
// not just the first, so a caller fixing a confirmed file list one path at a
// time sees every bad entry in one pass rather than resubmitting once per
// mistake.
func TestCheckResolvedFilesAggregatesEveryMalformedPath(t *testing.T) {
	t.Parallel()

	files := []ResolvedFile{
		{Path: "ok.go", Resolution: "package ok\n"},
		{Path: "", Resolution: "missing a path\n"},
		{Path: "-dashed.go", Resolution: "looks like a flag\n"},
	}

	err := checkResolvedFiles(files)
	if err == nil {
		t.Fatal("checkResolvedFiles accepted a list with two malformed entries")
	}
	// Both malformed entries must be represented, not just the first one
	// found (checkResolvedFiles uses errors.Join rather than failing fast).
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("error %v does not support multi-unwrap; want errors.Join of every malformed entry", err)
	}
	if got := len(joined.Unwrap()); got != 2 {
		t.Errorf("checkResolvedFiles joined %d errors, want 2 (one per malformed entry)", got)
	}
}

// TestCheckResolvedFilesRefusesAnEmptyList is [checkResolvedFiles]'s
// precondition: a confirmed resolution with no files at all is refused
// before anything else in [Worktree.WriteResolvedFiles] runs.
func TestCheckResolvedFilesRefusesAnEmptyList(t *testing.T) {
	t.Parallel()

	if err := checkResolvedFiles(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("checkResolvedFiles(nil) error = %v, want ErrInvalidArgument", err)
	}
}

// TestConflictSetMismatchReportsBothExtraAndMissingPaths is watch-out #7 of
// US-12, checked directly against the pure comparison rather than through a
// real merge: a confirmed list that both omits a real conflict and names an
// unconflicted path reports both problems in the one error, not just
// whichever direction the implementation happened to check first.
func TestConflictSetMismatchReportsBothExtraAndMissingPaths(t *testing.T) {
	t.Parallel()

	unmerged := map[string]struct{}{"a.go": {}, "b.go": {}}
	files := []ResolvedFile{{Path: "a.go", Resolution: "resolved a\n"}, {Path: "c.go", Resolution: "resolved c\n"}}

	err := conflictSetMismatch(unmerged, files)
	if !errors.Is(err, ErrConflictMismatch) {
		t.Fatalf("error = %v, want ErrConflictMismatch", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "b.go") {
		t.Errorf("error does not name the conflicted-but-unconfirmed path b.go: %s", msg)
	}
	if !strings.Contains(msg, "c.go") {
		t.Errorf("error does not name the confirmed-but-not-conflicted path c.go: %s", msg)
	}
}

// TestConflictSetMismatchAcceptsAnExactMatch is the mismatch check's
// negative case: identical sets report no error at all.
func TestConflictSetMismatchAcceptsAnExactMatch(t *testing.T) {
	t.Parallel()

	unmerged := map[string]struct{}{"a.go": {}}
	files := []ResolvedFile{{Path: "a.go", Resolution: "resolved a\n"}}

	if err := conflictSetMismatch(unmerged, files); err != nil {
		t.Errorf("conflictSetMismatch = %v, want nil for an exact match", err)
	}
}
