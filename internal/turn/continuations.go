package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// finishInterruption uses the adapter's terminal observation, not a new clock.
// The optional action and semantic failure become durable in one transaction.
func finishInterruption(ctx context.Context, opts Options, prep *preparation, res *agent.RunResult) (store.Run, error) {
	outcome := runOutcome(res)
	outcome.SemanticOutcome = "interrupted"
	if outcome.Error == "" {
		outcome.Error = "session limit interrupted execution"
	}
	reason := "Automatic session-limit recovery is disabled; resume manually after the limit lifts."
	var intent *store.Continuation
	token := res.ResumeToken
	if token == "" {
		token = prep.segment.ResumeToken
	}
	identity := token
	if prep.segment.Agent == store.AgentClaude {
		identity = prep.segment.SessionRef
	}
	prior, err := opts.DB.GetContinuationByRun(ctx, prep.run.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Run{}, err
	}
	if opts.AutoResumeOnSessionLimit {
		switch {
		case res.Interruption == nil || !res.Interruption.ResetBound.After(res.FinishedAt) || res.Interruption.DueAt.Before(res.Interruption.ResetBound.Add(time.Minute)):
			reason = "No reliable future reset time was supplied; resume manually after the limit lifts."
		case identity == "":
			reason = "No usable conversation identity was recorded; manual recovery is required."
		case prior.Attempt >= store.MaxAutomaticAttempts:
			reason = "The three automatic invocation limit is exhausted; manual recovery is required."
		default:
			snapshot := store.ContinuationContext{Task: prep.ref.String(), Worktree: prep.worktree.Path(), Agent: prep.segment.Agent, Mode: prep.run.Mode, Workflow: string(prep.request.Workflow), Request: prep.promptInput.Instruction, ResumeIdentity: identity, SessionRef: prep.segment.SessionRef, SegmentOrdinal: prep.segment.Ordinal, OwnerCommentID: prep.run.TriggerCommentID}
			if prep.promptInput.ApprovedStories != nil {
				snapshot.ApprovedGeneration = prep.promptInput.ApprovedStories.Generation
			}
			if prior.ID != 0 {
				snapshot = prior.Context
				snapshot.ResumeIdentity = identity
			}
			intent = &store.Continuation{Context: snapshot, DueAt: res.Interruption.DueAt, ResetBound: res.Interruption.ResetBound, Precision: string(res.Interruption.Precision)}
			if store.IsScheduleTrigger(prep.run.TriggerCommentID) {
				source, e := opts.DB.GetScheduleByTrigger(ctx, prep.run.TriggerCommentID)
				if e != nil {
					return store.Run{}, e
				}
				intent.PrerequisiteID = source.ID
				intent.Context.OwnerCommentID = source.SourceCommentID
			}
			reason = ""
		}
	}
	outcome.RecoveryReason = reason
	run, _, err := opts.DB.FinishInterruptedRun(ctx, prep.run.ID, outcome, intent, token)
	return run, err
}

// attachRecoveryReport reconstructs the announcement from durable state on both
// initial delivery and replay. It never creates scheduling authority from text.
func (r *Reporter) attachRecoveryReport(ctx context.Context, rep *RunReport) error {
	if rep.RunID == 0 {
		return nil
	}
	run, err := r.db.GetRun(ctx, rep.RunID)
	if err != nil {
		return err
	}
	if run.SemanticOutcome != "interrupted" {
		return nil
	}
	rep.Run.Recovery = &report.Recovery{ManualReason: run.RecoveryReason, InspectionCommand: report.RunInspectionCommand(rep.Run.Ref, run.Ordinal)}
	if rep.Run.Result == nil {
		exit := agent.SignalExitCode
		if run.ExitCode != nil {
			exit = int(*run.ExitCode)
		}
		rep.Run.Result = &agent.RunResult{Outcome: agent.OutcomeInterrupted, Reason: run.Error, ExitCode: exit, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt}
	}
	rep.Run.Result = rep.Run.Result.Clone()
	rep.Run.Result.Outcome = agent.OutcomeInterrupted
	c, err := r.db.GetContinuationBySource(ctx, run.ID)
	if errors.Is(err, store.ErrNotFound) {
		rep.Run.Recovery.NoContinuation = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("load recovery announcement: %w", err)
	}
	if c.AllocatedRunID > 0 {
		affected, e := r.db.GetRun(ctx, c.AllocatedRunID)
		if e != nil {
			return fmt.Errorf("load recovery run: %w", e)
		}
		if affected.SessionID != run.SessionID {
			return fmt.Errorf("recovery run belongs to a different session")
		}
		rep.Run.Recovery.InspectionCommand = report.RunInspectionCommand(rep.Run.Ref, affected.Ordinal)
	}
	rep.Run.Recovery.Status = c.Status
	if c.CancellationReason != "" {
		rep.Run.Recovery.ManualReason = c.CancellationReason
	}
	rep.Run.Recovery.ActionID = c.ID
	rep.Run.Recovery.DueAt = c.DueAt
	return nil
}
