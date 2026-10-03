package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

type graphSourceCheckpoint struct {
	Record    string             `json:"record"`
	State     string             `json:"state"`
	Operation string             `json:"operation"`
	Original  git.SourceIdentity `json:"original"`
	Options   git.SourceOptions  `json:"options"`
	Commit    git.CommitOptions  `json:"commit"`
	SavedHead string             `json:"saved_head,omitempty"`
}

// GraphBugSourcePreparation retains effective policy and actual source facts.
// Disabled synchronization is disclosed and never represented as a merge.
type GraphBugSourcePreparation struct {
	Record      string                `json:"record"`
	Source      git.SourceIdentity    `json:"source"`
	TargetMain  string                `json:"target_main"`
	SyncEnabled bool                  `json:"sync_enabled"`
	SyncOutcome string                `json:"sync_outcome"`
	SyncReason  string                `json:"sync_reason,omitempty"`
	SyncCommit  string                `json:"sync_commit,omitempty"`
	TaskSync    store.SyncObservation `json:"task_sync"`
}

// PrepareGraphBugSource runs only for a factory-admitted allocated simplified
// Bug agent. The host first fetches refs, freezes the slug and ensures the worktree
// under task ownership, releases its clone gate, then calls this seam before
// building either analyst or fixer prompt. Receiving a comment never calls it.
// It acquires a short clone gate and releases it before any agent invocation.
func PrepareGraphBugSource(ctx context.Context, opts Options, request Request, activity ActivityRequest, worktree *git.Worktree, repo store.Repo, author git.Author, logger *slog.Logger, at time.Time, sourceOptions git.SourceOptions) (GraphBugSourcePreparation, error) {
	var out GraphBugSourcePreparation
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || worktree == nil || logger == nil || at.IsZero() || activity.Workflow != agent.WorkflowBug || activity.Mode != "" || request.Workflow != agent.WorkflowBug || request.Mode != "" {
		return out, errors.New("simplified Bug source preparation requires admitted factory allocation")
	}
	attempt, e := opts.DB.GetGraphNodeAttempt(ctx, activity.NodeAttemptID)
	if e != nil {
		return out, e
	}
	instance, e := opts.DB.GetGraphInstance(ctx, activity.InstanceID)
	if e != nil {
		return out, e
	}
	if attempt.InstanceID != instance.ID || attempt.Node != activity.Node || attempt.RunID != activity.RunID || attempt.Status != store.GraphRunning || instance.SessionID != request.SessionID || request.researchInstance != instance.ID {
		return out, errors.New("source preparation allocation binding differs")
	}
	run, e := opts.DB.GetRun(ctx, activity.RunID)
	if e != nil {
		return out, e
	}
	if run.Status != store.RunRunning || run.SessionID != instance.SessionID || run.Workflow != "bug" || run.Mode != "" {
		return out, errors.New("source preparation requires active exact Bug run")
	}
	session, e := opts.DB.GetSession(ctx, instance.SessionID)
	if e != nil {
		return out, e
	}
	if session.RepoID != repo.ID || worktree.ClonePath() != repo.Path {
		return out, errors.New("source worktree or repository differs from allocated task")
	}
	clone, e := opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return out, e
	}
	release, e := opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return out, e
	}
	defer release()
	retain := func(record interface{}) error {
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		_, e = opts.DB.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: activity.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
		return e
	}
	raw, e := opts.DB.LatestGraphRecord(ctx, instance.ID, "source_checkpoint")
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return out, e
	}
	if e == nil {
		var checkpoint graphSourceCheckpoint
		if e = json.Unmarshal(raw, &checkpoint); e != nil {
			return out, e
		}
		if checkpoint.State == "pending" {
			saved, e := worktree.CheckpointSource(ctx, checkpoint.Original, checkpoint.Options, checkpoint.Commit)
			if e != nil {
				return out, fmt.Errorf("reconcile unvalidated checkpoint: %w", e)
			}
			checkpoint.State = "confirmed"
			checkpoint.SavedHead = saved.Head
			if e = retain(checkpoint); e != nil {
				return out, e
			}
		}
	}
	base := opts.remote() + "/" + repo.BaseBranch
	out.TargetMain, e = worktree.RevParse(ctx, base, git.RevParseOptions{})
	if e != nil {
		return out, e
	}
	divergence, e := worktree.Divergence(ctx, base)
	if e != nil {
		return out, e
	}
	taskBehind := 0
	taskHead, e := clone.ShowRef(ctx, "refs/remotes/"+opts.remote()+"/"+worktree.Branch())
	if e != nil && !errors.Is(e, git.ErrRefNotFound) {
		return out, e
	}
	if e == nil {
		taskDivergence, e := worktree.Divergence(ctx, taskHead)
		if e != nil {
			return out, e
		}
		taskBehind = taskDivergence.Behind
	}
	out.SyncEnabled = syncEnabled(opts, request)
	if taskBehind > 0 || (divergence.Behind > 0 && out.SyncEnabled && withinSyncCap(opts, divergence.Behind)) {
		changes, e := worktree.Status(ctx)
		if e != nil {
			return out, e
		}
		if len(changes) > 0 {
			// A preservation checkpoint binds every deliverable dirty byte,
			// including declared non-input logs/artifacts. Exclusions belong to
			// validation identity, not permission to lose retained work.
			checkpointOptions := sourceOptions
			checkpointOptions.NonInputs = nil
			original, e := worktree.ObserveSource(ctx, checkpointOptions)
			if e != nil {
				return out, e
			}
			operation := fmt.Sprintf("graph:%d:attempt:%d:source:%s", instance.ID, attempt.ID, original.Digest)
			checkpoint := graphSourceCheckpoint{Record: "source_checkpoint", State: "pending", Operation: operation, Original: original, Options: checkpointOptions, Commit: git.CommitOptions{Summary: "Unvalidated recovery checkpoint " + operation, Task: session.TaskRef, Workflow: "bug", Run: attempt.Attempt, Author: author}}
			if e = retain(checkpoint); e != nil {
				return out, fmt.Errorf("retain checkpoint intent before mutation: %w", e)
			}
			saved, e := worktree.CheckpointSource(ctx, original, checkpointOptions, checkpoint.Commit)
			if e != nil {
				return out, e
			}
			checkpoint.State = "confirmed"
			checkpoint.SavedHead = saved.Head
			if e = retain(checkpoint); e != nil {
				return out, e
			}
		}
	}
	task, e := syncTask(ctx, opts, request, clone, worktree, author, logger)
	out.TaskSync = observation(task)
	if e != nil {
		out.Record = "source_preparation"
		out.SyncOutcome = "blocked"
		out.SyncReason = e.Error()
		source, sourceErr := worktree.ObserveSource(ctx, sourceOptions)
		if sourceErr != nil {
			return out, errors.Join(e, sourceErr)
		}
		out.Source = source
		if retainErr := retain(out); retainErr != nil {
			return out, errors.Join(e, retainErr)
		}
		return out, e
	}
	sync, e := syncBase(ctx, opts, request, worktree, repo, author, logger)
	if e != nil {
		return out, e
	}
	out.Record = "source_preparation"
	out.SyncOutcome = string(sync.Outcome)
	out.SyncReason = sync.Reason
	out.SyncCommit = sync.SyncCommitSHA
	out.Source, e = worktree.ObserveSource(ctx, sourceOptions)
	if e != nil {
		return out, e
	}
	if e = retain(out); e != nil {
		return out, e
	}
	if sync.Outcome == syncOutcomeBlocked || sync.Outcome == syncOutcomeTooFarBehind || sync.Outcome == syncOutcomeDeferredDirty {
		return out, fmt.Errorf("bug synchronization cannot continue: %s %s", sync.Outcome, sync.Reason)
	}
	return out, nil
}
