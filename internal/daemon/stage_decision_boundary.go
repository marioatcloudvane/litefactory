package daemon

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// stageDecisionBoundary retains the daemon's live task-availability and ownership
// gates around the transport-neutral atomic workflow boundary.
type stageDecisionBoundary struct {
	daemon *Daemon
	local  stagedecision.Boundary
}

func (b stageDecisionBoundary) boundary() stagedecision.Boundary {
	if b.local != nil {
		return b.local
	}
	return turn.StageDecisionBoundary{DB: b.daemon.db}
}

func (b stageDecisionBoundary) ResolveStageDecisionTarget(ctx context.Context, task, stage, operation string) (store.StageDecisionTarget, error) {
	if b.daemon == nil || b.daemon.db == nil {
		return store.StageDecisionTarget{}, errors.New("stage decision availability is unavailable")
	}
	target, err := b.boundary().ResolveStageDecisionTarget(ctx, task, stage, operation)
	if err != nil {
		return target, err
	}
	if err = b.checkAvailability(ctx, target); err != nil {
		return store.StageDecisionTarget{}, err
	}
	return target, nil
}

func (b stageDecisionBoundary) ApplyStageDecision(ctx context.Context, target store.StageDecisionTarget, change, execution string, at time.Time) (store.StageDecisionReceipt, error) {
	if b.daemon == nil || b.daemon.db == nil {
		return store.StageDecisionReceipt{}, errors.New("stage decision availability is unavailable")
	}
	owner := "stage-decision:" + rand.Text()
	locked, err := b.daemon.db.AcquireSessionLock(ctx, target.SessionID, owner)
	if err != nil {
		return store.StageDecisionReceipt{}, err
	}
	if !locked {
		return store.StageDecisionReceipt{}, fmt.Errorf("%w: this task is busy; nothing was applied", store.ErrStageDecisionTarget)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := b.daemon.db.ReleaseSessionLock(cleanup, target.SessionID, owner); err != nil && b.daemon.log != nil {
			b.daemon.log.WarnContext(cleanup, "stage decision task lock release failed")
		}
	}()
	if err = b.checkAvailability(ctx, target); err != nil {
		return store.StageDecisionReceipt{}, err
	}
	return b.boundary().ApplyStageDecision(ctx, target, change, execution, at)
}

func (b stageDecisionBoundary) RecoverStageDecision(ctx context.Context, execution string, at time.Time) (store.StageDecisionReceipt, bool, error) {
	// Recovery reads the already committed receipt, even if the issue later closed.
	return b.boundary().RecoverStageDecision(ctx, execution, at)
}

func (b stageDecisionBoundary) checkAvailability(ctx context.Context, target store.StageDecisionTarget) error {
	d := b.daemon
	if d.forge == nil && d.connections == nil {
		return errors.New("the current task reader is unavailable")
	}
	session, err := d.db.GetSession(ctx, target.SessionID)
	if err != nil {
		return err
	}
	if session.TaskRef != target.TaskRef || session.RepoID != target.RepositoryID {
		return store.ErrStageDecisionTarget
	}
	repo, err := d.db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return err
	}
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	task := &task{repo: repo, session: session, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}}
	issue, err := d.readScheduledIssue(readCtx, task)
	if err != nil {
		return fmt.Errorf("read current stage task availability: %w", err)
	}
	if issue.State != github.StateOpen {
		return fmt.Errorf("%w: the task issue is closed; nothing was applied", store.ErrStageDecisionTarget)
	}
	// Bug is an independently admitted workflow, with no mode-label surrogate.
	if target.Workflow == "research" || target.Workflow == "plan" {
		mode, present, err := resolveModeLabel(issue.Labels)
		if err != nil {
			return fmt.Errorf("%w: the task's current mode is ambiguous or unavailable", store.ErrStageDecisionTarget)
		}
		if present && string(mode) != target.Workflow {
			return fmt.Errorf("%w: the task mode changed; nothing was applied", store.ErrStageDecisionTarget)
		}
	}
	return nil
}
