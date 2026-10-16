package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/git"
)

// SyncBaseCheckout is bound by the factory, never by graph attributes.
// Guard observes retained work before publication. Refresh verifies the exact
// successful working commit before the activity permits the next agent to run.
type SyncBaseCheckout interface {
	Guard(context.Context, string, string) (string, error)
	Refresh(context.Context, string, string) error
}

// GitSyncBaseCheckout preserves retained work by refusing unsafe updates and
// advancing only by fast-forward. The factory caller holds its clone gate.
type GitSyncBaseCheckout struct {
	Worktree *git.Worktree
	Author   git.Author
}

// Guard inspects retained bytes, branch identity and unpublished commits.
func (c GitSyncBaseCheckout) Guard(ctx context.Context, working, head string) (string, error) {
	if c.Worktree == nil {
		return "", errors.New("retained sync checkout is missing")
	}
	return c.Worktree.CheckSyncCheckout(ctx, working, head)
}

// Refresh advances a safe retained checkout to the exact successful commit.
func (c GitSyncBaseCheckout) Refresh(ctx context.Context, working, commit string) error {
	if c.Worktree == nil {
		return errors.New("retained sync checkout is missing")
	}
	reason, err := c.Worktree.CheckSyncCheckout(ctx, working, commit)
	if err != nil {
		return err
	}
	if reason != "" {
		return errors.New(reason)
	}
	result, err := c.Worktree.MergeBase(ctx, commit, git.MergeOptions{FastForwardOnly: true, Author: c.Author})
	if err != nil {
		return err
	}
	if result.Head != commit {
		return fmt.Errorf("retained checkout is at %s, expected %s", result.Head, commit)
	}
	head, err := c.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("retained checkout verification found %s, expected %s", head, commit)
	}
	return nil
}
