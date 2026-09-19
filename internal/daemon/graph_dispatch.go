package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// graphDispatchLimit bounds one tick's selection. A parked instance is owed one
// stage, not a queue, so a small bound is enough and keeps the pool available
// to owner comments, which are the work someone is waiting on.
const graphDispatchLimit = 8

// dispatchGraphSteps continues graph instances that published and ran out of
// run. One run advances one agent stage, so an instance that has more to do
// parks itself and leaves a dispatch behind; this is what turns that owed work
// back into a run. Owner comments keep priority: a session already busy is
// skipped and its dispatch stays pending for the next tick.
func (d *Daemon) dispatchGraphSteps(ctx context.Context, repos map[int64]store.Repo, at time.Time, result *TickResult) error {
	pending, err := d.db.ListDueGraphDispatches(ctx, at.UTC(), graphDispatchLimit)
	if err != nil {
		return err
	}
	for _, dispatch := range pending {
		session, err := d.db.GetSession(ctx, dispatch.SessionID)
		if err != nil {
			return err
		}
		repo, ok := repos[session.RepoID]
		if !ok || repo.Forge != store.ForgeGitHub {
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		log := d.log.With(
			slog.String("repo", repo.Owner+"/"+repo.Name),
			slog.String("task", ref.String()),
			slog.Int64("session_id", session.ID),
			slog.Int64("graph_dispatch_id", dispatch.ID),
			slog.String("graph_node", dispatch.Node))
		issue, err := d.forge.GetIssue(ctx, github.Repo{Owner: repo.Owner, Name: repo.Name}, session.IssueNumber)
		if err != nil {
			d.rateLimited(ctx, log, at, err)
			log.ErrorContext(ctx, "graph step issue observation failed; deferring", slog.Any("error", err))
			continue
		}
		// A closed issue withdraws the authority the instance was admitted
		// under. Cancelling the dispatch leaves the instance and its findings
		// exactly as they are; only the owed run goes away.
		if issue.State != github.StateOpen {
			if err := d.db.CancelGraphDispatch(ctx, dispatch.ID, "issue closed before the next graph stage could run", at.UTC()); err != nil {
				return err
			}
			continue
		}
		t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, issue: issue, log: log, tickAt: at}
		if _, held := d.backoff.hold(at); held {
			continue
		}
		if !d.inflight.enter(session.ID) {
			log.DebugContext(ctx, "graph step waits for this task's in-flight turn")
			continue
		}
		if !d.pool.try() {
			d.inflight.leave(session.ID)
			result.Deferred++
			continue
		}
		reserved, ok, err := d.db.ReserveGraphDispatch(ctx, dispatch.ID, at.UTC())
		if err != nil || !ok {
			d.pool.done()
			d.inflight.leave(session.ID)
			if err != nil {
				return err
			}
			continue
		}
		t.workerLog = log.With(slog.Any("dispatch_sources", []string{"graph_step"}), slog.Time("tick_at", at))
		t.workerLog.DebugContext(ctx, "worker dispatched")
		go d.runWorker(ctx, t, func() { d.runGraphStep(ctx, t, reserved) })
		result.Dispatched++
	}
	return nil
}

// runGraphStep executes one owed stage. The dispatch's private trigger is what
// gives this run its own identity on the runs table, the same way automatic
// recovery and manual schedules do; without one it could not exist beside the
// comment-triggered run that parked it.
func (d *Daemon) runGraphStep(ctx context.Context, t *task, dispatch store.GraphDispatch) {
	trigger := turn.Trigger{CommentID: dispatch.TriggerID(), Author: d.filter.Identities().OwnerHandle}
	command := turn.Command{Kind: turn.KindFreeText}
	err := d.run(ctx, t, trigger, command, t.log)
	if err != nil {
		t.recordWorkerError(err)
	}
	run, exists, lookupErr := d.db.GetRunByTrigger(ctx, dispatch.SessionID, dispatch.TriggerID())
	if lookupErr != nil {
		t.recordWorkerError(lookupErr)
		t.log.ErrorContext(ctx, "read graph step outcome", slog.Any("error", lookupErr))
		return
	}
	// No run means the step never started. The instance keeps its node and its
	// findings, so the work is still owed: requeue rather than cancel, unless
	// the reason it could not start will not go away on its own.
	if !exists || run.Status == store.RunPrelaunch {
		t.recordWorkerStatus(workerDeferred)
		if requeueErr := d.db.RequeueGraphDispatch(ctx, dispatch.ID); requeueErr != nil {
			t.recordWorkerError(requeueErr)
			t.log.ErrorContext(ctx, "requeue graph step", slog.Any("error", requeueErr))
		}
		return
	}
	if finishErr := d.db.FinishGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC()); finishErr != nil {
		t.recordWorkerError(finishErr)
		t.log.ErrorContext(ctx, "close graph step", slog.Any("error", finishErr))
	}
	t.log.InfoContext(ctx, "graph step finished", slog.String("status", string(run.Status)), slog.Int("run_ordinal", run.Ordinal))
}
