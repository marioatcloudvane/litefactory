package daemon

import (
	"context"
	"fmt"
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

// graphChainLimit bounds how many owed stages one worker runs back to back.
// The Plan creation cycle owes one per story; the bound only stops a runaway.
const graphChainLimit = 32

// continueGraphSteps runs the stages a session's graph instance owes right
// after the worker's own work, on the same slot. A stage parks the instance and
// leaves a dispatch that is due at once; waiting for the next tick for each one
// adds a minute per story to a Plan creation cycle. It also means a session the
// tick keeps busy, such as a graph Plan parent observed every tick for child
// replies, can never starve its own owed stage.
func (d *Daemon) continueGraphSteps(ctx context.Context, t *task) {
	if d.db == nil || d.forge == nil {
		return
	}
	for range graphChainLimit {
		// A stopped result still owed its repair holds the session; nothing
		// else runs on it until that succeeds.
		if ctx.Err() != nil || d.stoppedDebt.owes(t.session.ID) {
			return
		}
		if _, held := d.backoff.hold(t.tickAt); held {
			return
		}
		dispatch, ok, err := d.db.NextFreshGraphDispatch(ctx, t.session.ID)
		if err != nil || !ok {
			if err != nil {
				t.recordWorkerError(err)
				t.log.ErrorContext(ctx, "read the owed graph stage", slog.Any("error", err))
			}
			return
		}
		session, err := d.db.GetSession(ctx, dispatch.SessionID)
		if err != nil {
			t.recordWorkerError(err)
			t.log.ErrorContext(ctx, "read the session of the owed graph stage", slog.Any("error", err))
			return
		}
		log := t.log.With(slog.Int64("graph_dispatch_id", dispatch.ID), slog.String("graph_node", dispatch.Node))
		issue, err := d.forge.GetIssue(ctx, t.forgeRepo, session.IssueNumber)
		if err != nil {
			d.rateLimited(ctx, log, t.tickAt, err)
			log.ErrorContext(ctx, "graph step issue observation failed; leaving it to the tick", slog.Any("error", err))
			return
		}
		if issue.State != github.StateOpen {
			if err := d.db.CancelGraphDispatch(ctx, dispatch.ID, "issue closed before the next graph stage could run", t.tickAt.UTC()); err != nil {
				t.recordWorkerError(err)
			}
			return
		}
		// A parked stage is due from the instant it parked, which is later than
		// the tick this worker belongs to; reserving at that instant claims it.
		at := t.tickAt.UTC()
		if dispatch.DueAt.After(at) {
			at = dispatch.DueAt
		}
		reserved, ok, err := d.db.ReserveGraphDispatch(ctx, dispatch.ID, at)
		if err != nil || !ok {
			if err != nil {
				t.recordWorkerError(err)
			}
			return
		}
		log.InfoContext(ctx, "running the owed graph stage in the same worker")
		step := &task{repo: t.repo, forgeRepo: t.forgeRepo, session: session, ref: t.ref, issue: issue, log: log, tickAt: t.tickAt, worker: t.worker, workerLog: t.workerLog}
		d.runGraphStep(ctx, step, reserved)
	}
}

// runGraphStep executes one owed stage. The dispatch's private trigger is what
// gives this run its own identity on the runs table, the same way automatic
// recovery and manual schedules do; without one it could not exist beside the
// comment-triggered run that parked it.
func (d *Daemon) runGraphStep(ctx context.Context, t *task, dispatch store.GraphDispatch) {
	trigger, command := graphStepRequest(dispatch, d.filter.Identities().OwnerHandle)
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
		cause := ""
		if err != nil {
			cause = err.Error()
		}
		retryAt := t.tickAt.UTC().Add(store.GraphDispatchRetryDelay(dispatch.Attempts))
		requeued, requeueErr := d.db.RequeueGraphDispatch(ctx, dispatch.ID, retryAt, cause)
		if requeueErr != nil {
			t.recordWorkerError(requeueErr)
			t.log.ErrorContext(ctx, "requeue graph step", slog.Any("error", requeueErr))
			return
		}
		if !requeued {
			// Out of attempts. Saying so once is worth more than another tick
			// of the same failure, and the instance keeps its node and findings.
			t.log.ErrorContext(ctx, "graph step gave up after repeated failures to start",
				slog.Int("attempts", dispatch.Attempts), slog.String("cause", cause))
			return
		}
		t.log.WarnContext(ctx, "graph step could not start; retrying later",
			slog.Int("attempts", dispatch.Attempts), slog.Time("retry_at", retryAt), slog.String("cause", cause))
		return
	}
	if finishErr := d.db.FinishGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC()); finishErr != nil {
		t.recordWorkerError(finishErr)
		t.log.ErrorContext(ctx, "close graph step", slog.Any("error", finishErr))
	}
	t.log.InfoContext(ctx, "graph step finished", slog.String("status", string(run.Status)), slog.Int("run_ordinal", run.Ordinal))
}

// graphStepRequest builds the trigger and command one owed stage runs under.
//
// A turn refuses a free-text request with no instruction, and a dispatch has no
// comment to quote: it is the factory asking itself to continue. The stage's
// real instructions come from its role and the findings the instance already
// holds, so this text only names the work that asked for the run. It is
// separated out because the daemon cannot reach a turn's own validation, and an
// empty command here is not a compile error — it is a run that dies on arrival,
// every tick, which is exactly what it did before this had a test.
func graphStepRequest(dispatch store.GraphDispatch, owner string) (turn.Trigger, turn.Command) {
	instruction := fmt.Sprintf("Continue the workflow at its %s stage.", dispatch.Node)
	return turn.Trigger{CommentID: dispatch.TriggerID(), Body: instruction, Author: owner},
		turn.Command{Kind: turn.KindFreeText, Text: instruction}
}
