package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/git"
)

// ImplementRepo opens scratch worktrees on the integration branch. The merge
// activity owns the scratch tree it gets and closes it on every path.
type ImplementRepo interface {
	// Begin fetches the remote and checks out the integration head in a
	// scratch worktree. A missing integration branch is an error.
	Begin(ctx context.Context, integrationBranch string) (ImplementScratch, error)
}

// ImplementScratch is a throwaway worktree based on the integration head.
type ImplementScratch interface {
	// Head is the integration head the scratch tree was built on.
	Head() string
	// Path is the scratch worktree directory the checks run in.
	Path() string
	// Merge merges the story branch with a merge commit. A conflict is
	// git.ErrMergeConflict and leaves the tree as it was; merged is false when
	// the story is already part of the integration head.
	Merge(ctx context.Context, storyBranch string) (head string, merged bool, err error)
	// Publish pushes the merge as the integration branch without force, only
	// while the remote is still at expectedHead (git.ErrIntegrationHeadMoved otherwise).
	Publish(ctx context.Context, integrationBranch, expectedHead string) error
	// Close discards the scratch worktree.
	Close(ctx context.Context) error
}

// GitImplementRepo is the real repository behind ImplementRepo.
type GitImplementRepo struct {
	Clone       *git.Clone
	Remote      string
	ScratchRoot string
	Author      git.Author
}

// Begin implements ImplementRepo.
func (r GitImplementRepo) Begin(ctx context.Context, integrationBranch string) (ImplementScratch, error) {
	if r.Clone == nil || r.Remote == "" || r.ScratchRoot == "" || integrationBranch == "" {
		return nil, errors.New("git implement repository is not configured")
	}
	if err := r.Clone.Fetch(ctx, r.Remote); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.Remote, err)
	}
	head, err := r.Clone.RevParse(ctx, "refs/remotes/"+r.Remote+"/"+integrationBranch, git.RevParseOptions{})
	if err != nil {
		return nil, fmt.Errorf("integration branch %s: %w", integrationBranch, err)
	}
	if err := os.MkdirAll(r.ScratchRoot, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(r.ScratchRoot, "implement-merge-")
	if err != nil {
		return nil, err
	}
	tree := filepath.Join(dir, "tree")
	wt, err := r.Clone.WorktreeAdd(ctx, tree, git.WorktreeAddOptions{Branch: head, Detach: true})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("scratch worktree: %w", err), os.RemoveAll(dir))
	}
	return &gitScratch{repo: r, wt: wt, dir: dir, head: head}, nil
}

type gitScratch struct {
	repo GitImplementRepo
	wt   *git.Worktree
	dir  string
	head string
}

func (s *gitScratch) Head() string { return s.head }
func (s *gitScratch) Path() string { return s.wt.Path() }

func (s *gitScratch) Merge(ctx context.Context, storyBranch string) (string, bool, error) {
	res, err := s.wt.MergeBase(ctx, "refs/remotes/"+s.repo.Remote+"/"+storyBranch, git.MergeOptions{Author: s.repo.Author})
	if err != nil {
		return "", false, err
	}
	return res.Head, res.Merged, nil
}

func (s *gitScratch) Publish(ctx context.Context, integrationBranch, expectedHead string) error {
	return s.wt.PushHeadTo(ctx, s.repo.Remote, integrationBranch, expectedHead)
}

func (s *gitScratch) Close(ctx context.Context) error {
	removeErr := s.repo.Clone.WorktreeRemove(ctx, s.wt.Path(), git.WorktreeRemoveOptions{Force: true})
	return errors.Join(removeErr, os.RemoveAll(s.dir))
}
