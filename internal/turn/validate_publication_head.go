package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidatePublicationHead observes the local feature ref that the Validate
// report displays. It neither fetches nor opens, changes or repairs a worktree.
func ValidatePublicationHead(ctx context.Context, opts Options, session store.Session) (string, error) {
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || strings.TrimSpace(session.Branch) == "" {
		return "", errors.New("the displayed Validate branch cannot be observed")
	}
	repo, err := executionRepository(ctx, opts.DB, session)
	if err != nil {
		return "", err
	}
	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	release, err := opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	defer release()
	if err := verifyExecutionClone(ctx, opts, session, clone); err != nil {
		return "", err
	}
	head, err := clone.RevParse(ctx, "refs/heads/"+session.Branch, git.RevParseOptions{})
	if err != nil {
		return "", err
	}
	if !isFullSHA(head) {
		return "", fmt.Errorf("displayed Validate branch %s has no full commit", session.Branch)
	}
	return head, nil
}
