package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Inspect every pending action, including future deadlines. Cancellation is
// durable so reopening an issue or enabling the option cannot resurrect work.
func (d *Daemon) dispatchContinuations(ctx context.Context, repos map[int64]store.Repo, at time.Time, result *TickResult) error {
	pending, err := d.db.ListPendingContinuations(ctx, 0)
	if err != nil {
		return err
	}
	for _, c := range pending {
		graph, err := d.db.HasWorkflowGraphSession(ctx, c.SessionID, c.Context.Mode)
		if err != nil {
			return err
		}
		if graph && (c.Context.Mode == "research" || c.Context.Mode == "plan" || c.Context.Mode == "spike") {
			if err := d.db.CancelContinuation(ctx, c.ID, "Workflow graph requires explicit owner continuation", at); err != nil {
				return err
			}
			continue
		}
		if !d.autoResumeOnSessionLimit {
			if err := d.db.CancelContinuation(ctx, c.ID, "automatic recovery disabled; resume manually", at); err != nil {
				return err
			}
			continue
		}
		session, err := d.db.GetSession(ctx, c.SessionID)
		if err != nil {
			return err
		}
		if d.sessionRecoveryHeld(session.ID, at) {
			continue
		}
		repo, ok := repos[session.RepoID]
		if !ok || repo.Forge != store.ForgeGitHub {
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: d.log.With(
			slog.String("repo", repo.Owner+"/"+repo.Name),
			slog.String("task", ref.String()),
			slog.Int64("session_id", session.ID),
			slog.Int64("continuation_id", c.ID)), tickAt: at, continuation: &c}
		ready, err := d.observeContinuation(ctx, t, c)
		if err != nil {
			d.rateLimited(ctx, t.log, at, err)
			t.log.ErrorContext(ctx, "automatic recovery observation failed; deferring", slog.Any("error", err))
			continue
		}
		if !ready || c.DueAt.After(at.UTC()) {
			continue
		}
		if _, held := d.backoff.hold(at); held {
			continue
		}
		if !d.enterDoctorGuarded(ctx, session.ID) {
			t.log.DebugContext(ctx, "automatic recovery waits for this task's in-flight turn")
			continue
		}
		if !d.pool.try() {
			d.inflight.leave(session.ID)
			result.Deferred++
			continue
		}
		reserved, ok, err := d.db.ReserveContinuation(ctx, c.ID, at.UTC())
		if err != nil || !ok {
			d.pool.done()
			d.inflight.leave(session.ID)
			if err != nil {
				return err
			}
			continue
		}
		t.continuation = &reserved
		t.workerLog = t.log.With(slog.Any("dispatch_sources", []string{"automatic_continuation"}), slog.Time("tick_at", at))
		t.workerLog.DebugContext(ctx, "worker dispatched")
		// This worker belongs to the same bounded pool and Quiesce lifetime as
		// ordinary and manually scheduled turns.
		go d.runDoctorGuardedWorker(ctx, t, func(ctx context.Context) { d.runContinuation(ctx, t, reserved) })
		result.Dispatched++
	}
	return nil
}

// Admission records classification, but never consumes the ordinary watermark.
func (d *Daemon) observeContinuation(ctx context.Context, t *task, c store.Continuation) (bool, error) {
	cancel := func(reason string) (bool, error) {
		return false, d.db.CancelContinuation(ctx, c.ID, reason+"; resume manually", t.tickAt)
	}
	issue, err := d.readIssue(ctx, t.forgeRepo, t.session.IssueNumber)
	if err != nil {
		return false, err
	}
	t.issue = issue
	if issue.State != github.StateOpen {
		return cancel("issue closed")
	}
	if t.session.TaskRef != c.Context.Task || t.session.WorktreePath != c.Context.Worktree {
		return cancel("task context changed")
	}
	choice := d.resolveAgent(issue.Labels)
	selected := choice.Agent
	if selected == "" {
		selected = string(d.defaultAgent)
	}
	if choice.refused() || selected != c.Context.Agent {
		return cancel("coding agent changed")
	}
	mode, labelled, err := resolveModeLabel(issue.Labels)
	if err != nil || (labelled && string(mode) != c.Context.Mode) {
		return cancel("workflow mode changed")
	}
	segments, err := d.db.ListSegments(ctx, c.SessionID)
	if err != nil {
		return false, err
	}
	if len(segments) == 0 || segments[len(segments)-1].ID != c.SegmentID {
		return cancel("conversation reset")
	}
	segment := segments[len(segments)-1]
	identity := segment.ResumeToken
	if segment.Agent == store.AgentClaude {
		identity = segment.SessionRef
	}
	if identity == "" || identity != c.Context.ResumeIdentity {
		return cancel("conversation identity unavailable or changed")
	}
	comments, err := d.readComments(ctx, t.forgeRepo, issue.Number, c.Context.OwnerCommentID)
	if err != nil {
		return false, err
	}
	for _, comment := range comments {
		if comment.ID <= c.Context.OwnerCommentID {
			continue
		}
		qualifiedReply, err := d.qualifyReply(ctx, c.SessionID, comment)
		if err != nil {
			return false, err
		}
		admission, err := d.admitReply(ctx, c.SessionID, comment, qualifiedReply)
		if err != nil {
			return false, err
		}
		if admission.Triggers() && turn.ParseCommand(comment.Body).Spawns() {
			return cancel("new owner instruction superseded recovery")
		}
	}
	return true, nil
}

func (d *Daemon) runContinuation(ctx context.Context, t *task, c store.Continuation) {
	// A final observation occurs immediately before the existing locked turn
	// path. New work after this boundary follows ordinary already-running rules.
	ready, err := d.observeContinuation(ctx, t, c)
	if err != nil || !ready {
		if err != nil {
			t.recordWorkerStatus(workerDeferred)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.recordWorkerStatus(workerCancelled)
			}
			t.log.ErrorContext(ctx, "final recovery observation failed", slog.Any("error", err))
			if e := d.db.RequeueContinuation(ctx, c.ID); e != nil {
				t.recordWorkerError(e)
				t.log.ErrorContext(ctx, "requeue recovery", slog.Any("error", e))
			}
		}
		return
	}
	trigger := turn.Trigger{CommentID: c.TriggerID(), Body: c.Context.Request, Author: d.filter.Identities().OwnerHandle}
	command := turn.Command{Kind: turn.KindFreeText, Text: c.Context.Request}
	err = d.run(ctx, t, trigger, command, t.log)
	if err != nil {
		t.recordWorkerError(err)
	}
	run, exists, lookupErr := d.db.GetRunByTrigger(ctx, c.SessionID, c.TriggerID())
	if lookupErr != nil {
		t.recordWorkerError(lookupErr)
		t.log.ErrorContext(ctx, "read automatic outcome", slog.Any("error", lookupErr))
		return
	}
	if exists && run.Status == store.RunPrelaunch {
		t.recordWorkerStatus(workerDeferred)
		if requeueErr := d.db.RequeueContinuation(ctx, c.ID); requeueErr != nil {
			t.recordWorkerError(requeueErr)
			t.log.ErrorContext(ctx, "requeue prelaunch recovery", slog.Any("error", requeueErr))
		}
		return
	}
	if !exists {
		if err == nil && t.continuationLocked {
			t.recordWorkerStatus(workerDeferred)
			err = d.db.RequeueContinuation(ctx, c.ID)
		} else {
			t.recordWorkerStatus(workerFailed)
			err = d.db.CancelContinuation(ctx, c.ID, fmt.Sprintf("automatic recovery could not start; resume manually: %v", err), t.tickAt)
		}
		if err != nil {
			t.recordWorkerError(err)
			t.log.ErrorContext(ctx, "record automatic admission outcome", slog.Any("error", err))
		}
	}
}
