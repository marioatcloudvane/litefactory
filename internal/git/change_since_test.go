package git_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestChangedSinceIsTheUniqueUnionOfCommittedStagedUnstagedAndUntracked(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("keep.go", "package a\n")
	repo.WriteFile("committed.go", "package a\n")
	repo.WriteFile("staged.go", "package a\n")
	repo.WriteFile("unstaged.go", "package a\n")
	repo.WriteFile("both.go", "package a\n")
	repo.WriteFile("gone.go", "package a\n")
	repo.WriteFile("moved.go", "package a\n")
	base := repo.Commit("baseline")
	repo.Git("push", "origin", "main")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, dir := task(t, worktreeRoot(repo), 406, "change since")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	worktree := result.Worktree
	if changed, err := worktree.ChangedSince(t.Context(), base); err != nil || len(changed) != 0 {
		t.Fatalf("an untouched worktree changed: %v %v", changed, err)
	}
	write := func(name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n// changed "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("committed.go")
	write("both.go")
	repo.GitIn(dir, "add", "committed.go", "both.go")
	repo.GitIn(dir, "commit", "-m", "commit some")
	write("both.go")
	write("staged.go")
	repo.GitIn(dir, "add", "staged.go")
	write("unstaged.go")
	write("fresh/untracked.go")
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	repo.GitIn(dir, "mv", "moved.go", "renamed.go")
	got, err := worktree.ChangedSince(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"both.go", "committed.go", "fresh/untracked.go", "gone.go", "moved.go", "renamed.go", "staged.go", "unstaged.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	for _, bad := range []string{"", "HEAD", "abc", base[:12]} {
		if _, err := worktree.ChangedSince(t.Context(), bad); err == nil {
			t.Fatalf("baseline %q was accepted", bad)
		}
	}
}

func TestFilesChangedByListsAMergeAgainstItsFirstParentAndNothingForARoot(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("shared.go", "package a\n")
	repo.Commit("baseline")
	repo.Git("push", "origin", "main")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, dir := task(t, worktreeRoot(repo), 974, "files changed by")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	worktree := result.Worktree
	write := func(name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n// changed "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repo.GitIn(dir, "checkout", "--quiet", "-b", "story")
	write("story/one.go")
	write("story/two.go")
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "-m", "story work")
	repo.GitIn(dir, "checkout", "--quiet", branch)
	write("feature.go")
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "-m", "feature work")
	repo.GitIn(dir, "merge", "--quiet", "--no-ff", "--no-edit", "story")
	merge := repo.GitIn(dir, "rev-parse", "HEAD")
	got, err := worktree.FilesChangedBy(t.Context(), merge)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"story/one.go", "story/two.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merge changed = %v, want %v", got, want)
	}
	root := repo.GitIn(dir, "rev-list", "--max-parents=0", "HEAD")
	if got, err := worktree.FilesChangedBy(t.Context(), root); err != nil || len(got) != 0 {
		t.Fatalf("root commit changed = %v %v, want nothing", got, err)
	}
	for _, bad := range []string{"", "HEAD", "abc", merge[:12]} {
		if _, err := worktree.FilesChangedBy(t.Context(), bad); err == nil {
			t.Fatalf("commit %q was accepted", bad)
		}
	}
}
