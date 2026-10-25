package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// PrepareGraphBugSource readies the task worktree for one Bug agent and
// returns a short note for its prompt. Synchronization observations are retained
// on the current run for ordinary result delivery.
//
// The analyst reads the worktree as it is. Fixer preparation observes task
// and base freshness without committing retained edits. Known unresolved
// conditions continue with a caveat; operational and identity errors still fail.
func PrepareGraphBugSource(ctx context.Context, opts Options, request Request, activity ActivityRequest, worktree *git.Worktree, repo store.Repo, author git.Author, logger *slog.Logger, at time.Time, synchronize bool) (string, error) {
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || worktree == nil || logger == nil || at.IsZero() || activity.Workflow != agent.WorkflowBug || activity.Mode != "" || request.Workflow != agent.WorkflowBug || request.Mode != "" {
		return "", errors.New("source preparation requires an admitted Bug factory allocation")
	}
	attempt, e := opts.DB.GetGraphNodeAttempt(ctx, activity.NodeAttemptID)
	if e != nil {
		return "", e
	}
	if attempt.InstanceID != activity.InstanceID || attempt.Node != activity.Node || attempt.RunID != activity.RunID || attempt.Status != store.GraphRunning || request.researchInstance != activity.InstanceID {
		return "", errors.New("source preparation allocation binding differs")
	}
	session, e := opts.DB.GetSession(ctx, request.SessionID)
	if e != nil {
		return "", e
	}
	if session.RepoID != repo.ID || worktree.ClonePath() != repo.Path {
		return "", errors.New("source worktree or repository differs from allocated task")
	}
	clone, e := opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	release, e := opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	defer release()
	var note strings.Builder
	if synchronize {
		if e = clone.Fetch(ctx, opts.remote()); e != nil {
			return "", e
		}
		task, e := syncTask(ctx, opts, request, clone, worktree, author, logger)
		if e != nil {
			return "", e
		}
		local, e := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
		if e != nil {
			return "", e
		}
		baseRef := opts.remote() + "/" + repo.BaseBranch
		baseSHA, e := worktree.RevParse(ctx, baseRef, git.RevParseOptions{})
		if e != nil {
			return "", e
		}
		var base syncResult
		if preparationSyncDeferred(task) {
			base, e = observeDeferredBase(ctx, opts, worktree, baseSHA)
		} else {
			base, e = syncBase(ctx, opts, request, worktree, repo, author, logger)
		}
		if e != nil {
			return "", e
		}
		base.Phase, base.Base, base.BranchSHA, base.BaseSHA = "base", baseRef, local, baseSHA
		if _, e = opts.DB.RecordRunSyncObservations(ctx, attempt.RunID, []store.SyncObservation{observation(task), observation(base)}); e != nil {
			return "", e
		}
		for _, s := range []syncResult{task, base} {
			if preparationSyncDeferred(s) {
				fmt.Fprintf(&note, "%s synchronization remains unresolved: %s. Existing edits and branch were preserved; continue only authorized repair work.\n", s.Phase, s.Reason)
			}
		}

	}
	head, e := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		return "", e
	}
	fmt.Fprintf(&note, "The worktree is at commit %s on branch %s.\n", head, worktree.Branch())
	return note.String(), nil
}
