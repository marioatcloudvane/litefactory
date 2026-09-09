package git_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestWorktreeStatusPreservesRawIndexOnStatRefresh(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, dir := task(t, worktreeRoot(repo), 171, "retained edits")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	indexPath := repo.GitIn(dir, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(dir, indexPath)
	}
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(dir, "README.md")
	info, err := os.Stat(tracked)
	if err != nil {
		t.Fatal(err)
	}
	stamp := info.ModTime().Add(10 * time.Second)
	if err := os.Chtimes(tracked, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	changes, err := result.Worktree.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("status refreshed the raw index")
	}
}
