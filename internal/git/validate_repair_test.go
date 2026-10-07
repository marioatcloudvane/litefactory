package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func repairWorktree(t *testing.T) (*git.Worktree, string, string, *gittest.Repo) {
	t.Helper()
	repo := gittest.New(t)
	repo.WriteFile("keep.go", "package a\n")
	repo.WriteFile("edit.go", "package a\n\nvar X = 1\n")
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
	branch, dir := task(t, worktreeRoot(repo), 497, "repair")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	return result.Worktree, dir, base, repo
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiffSinceShowsEditsAndListsUntrackedFiles(t *testing.T) {
	w, dir, base, _ := repairWorktree(t)
	if text, err := w.DiffSince(t.Context(), base); err != nil || strings.TrimSpace(text) != "" {
		t.Fatalf("an untouched worktree has a diff: %q %v", text, err)
	}
	put(t, dir, "edit.go", "package a\n\nvar X = 2\n")
	put(t, dir, "fresh/new.go", "package fresh\n")
	text, err := w.DiffSince(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/edit.go b/edit.go", "-var X = 1", "+var X = 2", "new untracked file: fresh/new.go"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	for _, bad := range []string{"HEAD", "main", "", "zzzz", strings.Repeat("0", 40)} {
		if _, err := w.DiffSince(t.Context(), bad); err == nil {
			t.Errorf("baseline %q was accepted", bad)
		}
	}
}

func TestResetHardRestoresTheCommitAndKeepsOnlyTheNamedPaths(t *testing.T) {
	w, dir, base, repo := repairWorktree(t)
	put(t, dir, "edit.go", "package a\n\nvar X = 99\n")
	put(t, dir, "half/made.go", "package half\n")
	put(t, dir, ".aiteam/session/notes.md", "keep\n")
	repo.GitIn(dir, "add", "edit.go")
	if err := w.ResetHard(t.Context(), base, ".aiteam/session"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "edit.go"))
	if err != nil || string(got) != "package a\n\nvar X = 1\n" {
		t.Fatalf("edit.go %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "half", "made.go")); !os.IsNotExist(err) {
		t.Fatalf("an untracked file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".aiteam/session/notes.md")); err != nil {
		t.Fatalf("a kept path was removed: %v", err)
	}
	// A reset also undoes a commit made after the base, and moves the branch back.
	put(t, dir, "edit.go", "package a\n\nvar X = 7\n")
	repo.GitIn(dir, "add", "edit.go")
	repo.GitIn(dir, "commit", "-m", "later")
	if head, _ := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{}); head == base {
		t.Fatal("the commit was not made")
	}
	if err := w.ResetHard(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	if head, _ := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{}); head != base {
		t.Fatalf("head %s, want %s", head, base)
	}
	for _, bad := range []string{"HEAD", "", "main", strings.Repeat("0", 40)} {
		if err := w.ResetHard(t.Context(), bad); err == nil {
			t.Errorf("target %q was accepted", bad)
		}
	}
}
