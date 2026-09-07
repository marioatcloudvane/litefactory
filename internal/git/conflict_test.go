package git_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// TestWorktreeConflictBlobReadsConflictMarkers is US-11: the tree
// [Worktree.MergePreview] computed for a conflicted merge carries, at the
// conflicting path, content wrapped in the ordinary merge conflict markers —
// exactly what a real working-tree conflict would have left — and reading it
// touches neither the ref, the branch nor the worktree.
func TestWorktreeConflictBlobReadsConflictMarkers(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Conflicted preview")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	var preview git.MergePreview
	var err error
	gittest.AssertUnchanged(t, dir, func() {
		preview, err = worktree.MergePreview(t.Context(), base)
	})
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}
	if preview.Clean {
		t.Fatal("Clean = true, want false")
	}

	var content string
	gittest.AssertUnchanged(t, dir, func() {
		content, err = worktree.ConflictBlob(t.Context(), preview.TreeSHA, "README.md")
	})
	if err != nil {
		t.Fatalf("ConflictBlob: %v", err)
	}
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if !strings.Contains(content, marker) {
			t.Errorf("conflict blob missing marker %q; content = %q", marker, content)
		}
	}
}

func TestWorktreeConflictBlobRefusesUnscreenedNames(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, _ := newTaskWorktree(t, repo, 43, "Conflict blob refusal")

	if _, err := worktree.ConflictBlob(t.Context(), "--upload-pack=evil", "README.md"); err == nil {
		t.Fatal("accepted a tree sha starting with a dash")
	}
	if _, err := worktree.ConflictBlob(t.Context(), "HEAD", "-rf"); err == nil {
		t.Fatal("accepted a path starting with a dash")
	}
}
