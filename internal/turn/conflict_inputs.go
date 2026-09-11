package turn

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// observeConflictInputs is read-only and is called only under prepareClone's
// gate after the authoritative fetch. Absence is a bound input, not a wildcard.
func observeConflictInputs(ctx context.Context, opts Options, clone *git.Clone, w *git.Worktree, repo store.Repo) (store.ConflictProposalInputs, error) {
	p := store.ConflictProposalInputs{BaseRef: opts.remote() + "/" + repo.BaseBranch, TaskRef: opts.remote() + "/" + w.Branch(), Observed: true}
	var err error
	p.BaseSHA, err = clone.ShowRef(ctx, "refs/remotes/"+p.BaseRef)
	if err != nil {
		return p, fmt.Errorf("observe conflict base input: %w", err)
	}
	p.TaskSHA, err = clone.ShowRef(ctx, "refs/remotes/"+p.TaskRef)
	if errors.Is(err, git.ErrRefNotFound) {
		p.TaskSHA = ""
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("observe conflict task input: %w", err)
	}
	return p, nil
}
