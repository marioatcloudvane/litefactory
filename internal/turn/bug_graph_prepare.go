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
// returns a short note for its prompt. Git is the record: nothing about the
// source is stored in the factory database.
//
// The analyst reads the worktree as it is. The fixer first commits any work
// left in the worktree, then synchronizes with the task branch and main; a
// conflict or a branch too far behind is told to the fixer, never a refusal.
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
		// Preserve earlier work in git before synchronizing; the commit is
		// part of the branch the factory later delivers.
		if _, e = worktree.Commit(ctx, git.CommitOptions{Summary: "Preserve Bug work before synchronizing", Task: session.TaskRef, Workflow: "bug", Run: attempt.Attempt, Author: author}); e != nil {
			return "", e
		}
		task, e := syncTask(ctx, opts, request, clone, worktree, author, logger)
		if e != nil {
			fmt.Fprintf(&note, "The task branch could not be synchronized with its remote copy: %s.\n", e.Error())
		} else if task.Outcome == syncOutcomeBlocked {
			fmt.Fprintf(&note, "The task branch could not be synchronized with its remote copy: %s.\n", task.Reason)
		}
		base, e := syncBase(ctx, opts, request, worktree, repo, author, logger)
		if e != nil {
			return "", e
		}
		switch base.Outcome {
		case syncOutcomeBlocked:
			fmt.Fprintf(&note, "The branch was not merged with %s: it conflicts in %s. Repair on the current branch; delivery will try the validated conflict advisor, then return here for fresh checks or ask the owner if resolution fails.\n", repo.BaseBranch, strings.Join(base.Conflicts, ", "))
		case syncOutcomeTooFarBehind, syncOutcomeDeferredDirty, syncOutcomeSkipped:
			if base.Reason != "" {
				fmt.Fprintf(&note, "The branch was not merged with %s: %s.\n", repo.BaseBranch, base.Reason)
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
