package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
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
	workerCtx := ctx
	ctx, cancel := d.tickPhaseContext(ctx)
	defer cancel()
	// Coordinator bookkeeping is selected on its own first, so story dispatches
	// blocked on run capacity cannot keep it out of this tick's window.
	coordinators, err := d.db.ListDueCoordinatorGraphDispatches(ctx, at.UTC(), graphDispatchLimit)
	if err != nil {
		return err
	}
	general, err := d.db.ListDueGraphDispatches(ctx, at.UTC(), graphDispatchLimit)
	if err != nil {
		return err
	}
	seen := make(map[int64]bool, len(coordinators)+len(general))
	for _, dispatch := range append(coordinators, general...) {
		if seen[dispatch.ID] {
			continue
		}
		seen[dispatch.ID] = true
		ctx := sessionContext(ctx, dispatch.SessionID)
		if d.bug != nil {
			if err := d.bug.CheckExistingAdmission(ctx, dispatch.SessionID); err != nil {
				d.log.ErrorContext(ctx, "graph dispatch refused; retained work unchanged", slog.Int64("dispatch_id", dispatch.ID), slog.Any("error", err))
				continue
			}
		}

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
		// The read is pinned to the session's retained ticket container and
		// code target, as scheduled work is; a Linear ticket never lives at
		// the code repository.
		t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: log, tickAt: at}
		issue, err := d.readScheduledIssue(ctx, t)
		if err != nil {
			d.rateLimited(ctx, log, at, err)
			log.ErrorContext(ctx, "graph step issue observation failed; deferring", slog.Any("error", err))
			continue
		}
		// A closed issue withdraws the authority the instance was admitted
		// under. Cancelling the dispatch leaves the instance and its findings
		// exactly as they are; only the owed run goes away.
		bugParent, err := d.isBugParentDispatch(ctx, dispatch)
		if err != nil {
			return err
		}
		if issue.State != github.StateOpen && !bugParent {
			if err := d.db.CancelGraphDispatch(ctx, dispatch.ID, "issue closed before the next graph stage could run", at.UTC()); err != nil {
				return err
			}
			continue
		}
		t.issue = issue
		if _, held := d.connectionHold(ctx, at); held {
			continue
		}
		if !d.enterDoctorGuarded(ctx, session.ID) {
			log.DebugContext(ctx, "graph step waits for this task's in-flight turn")
			continue
		}
		slot, deferral, err := d.graphStepSlot(ctx, dispatch)
		if err != nil {
			d.inflight.leave(session.ID)
			log.ErrorContext(ctx, "graph capacity inspection failed; retained work unchanged", slog.Any("error", err))
			continue
		}
		if slot == nil {
			d.inflight.leave(session.ID)
			result.Deferred++
			log.InfoContext(ctx, "graph step deferred for capacity", slog.String("reason", deferral))
			continue
		}
		t.slot = slot.pool
		t.capacity = slot
		t.slot.wg.Add(1)
		reserved, ok, err := d.db.ReserveGraphDispatch(ctx, dispatch.ID, at.UTC())
		if err != nil || !ok {
			slot.Release()
			t.slot.wg.Done()
			d.inflight.leave(session.ID)
			if err != nil {
				return err
			}
			continue
		}
		d.inflight.label(session.ID, "graph_step")
		t.workerLog = log.With(slog.Any("dispatch_sources", []string{"graph_step"}), slog.Time("tick_at", at))
		t.workerLog.DebugContext(ctx, "worker dispatched")
		go d.runDoctorGuardedWorker(sessionContext(workerCtx, dispatch.SessionID), t, func(ctx context.Context) { d.runGraphStep(ctx, t, reserved) })
		result.Dispatched++
	}
	return nil
}

// graphStepSlot takes the slot a due dispatch runs on, or reports why none was
// free. Implement coordinator bookkeeping tries its reserved slot first and
// falls back to the run pool; an agent stage, a step that cannot be classified,
// and every other dispatch use the run pool only.
func (d *Daemon) graphStepSlot(ctx context.Context, dispatch store.GraphDispatch) (*stepReservation, string, error) {
	cost, err := turn.GraphDispatchSlotCost(ctx, d.db, dispatch)
	if err != nil {
		return nil, "", err
	}
	if d.coordinator != nil {
		coordinator, err := d.implementCoordinatorDispatch(ctx, dispatch)
		if err != nil {
			return nil, "", err
		}
		if coordinator && !d.graphStepRunsAgent(ctx, dispatch) && d.coordinator.acquire(cost) {
			return &stepReservation{pool: d.coordinator, fallback: d.pool, cost: cost}, "", nil
		}
	}
	if d.pool.acquire(cost) {
		return &stepReservation{pool: d.pool, cost: cost}, "", nil
	}
	used, total := d.pool.capacity()
	reason := "temporary capacity deferral"
	if cost > total {
		reason = "impossible capacity fit"
	}
	return nil, fmt.Sprintf("%s: pool=shared workflow_instance=%d node=%s requested=%d used=%d available=%d total=%d", reason, dispatch.InstanceID, dispatch.Node, cost, used, total-used, total), nil
}

// graphStepRunsAgent classifies a dispatch by its node in the retained graph. A
// step that cannot be classified counts as an agent step.
func (d *Daemon) graphStepRunsAgent(ctx context.Context, dispatch store.GraphDispatch) bool {
	runsAgent, err := turn.GraphDispatchRunsAgent(ctx, d.db, dispatch)
	if err != nil {
		d.log.DebugContext(ctx, "graph step not classified; treating it as an agent step", slog.Int64("graph_dispatch_id", dispatch.ID), slog.Any("error", err))
		return true
	}
	return runsAgent
}

// graphChainLimit bounds how many owed stages one worker runs back to back.
// The Plan creation cycle owes one per story; the bound only stops a runaway.
const graphChainLimit = 32

// continueGraphSteps runs the stages a session's graph instance owes right
// after the worker's own work, with fresh weighted admission for each step. A stage parks the instance and
// leaves a dispatch that is due at once; waiting for the next tick for each one
// adds a minute per story to a Plan creation cycle. It also means a session the
// tick keeps busy, such as a graph Plan parent observed every tick for child
// replies, can never starve its own owed stage.
func (d *Daemon) continueGraphSteps(ctx context.Context, t *task) {
	ctx = sessionContext(ctx, t.session.ID)
	if d.db == nil || d.forge == nil {
		return
	}
	for range graphChainLimit {
		// A stopped result still owed its repair holds the session; nothing
		// else runs on it until that succeeds.
		if ctx.Err() != nil || d.stoppedDebt.owes(t.session.ID) {
			return
		}
		if _, held := d.connectionHold(ctx, t.tickAt); held {
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
		// A reserved coordinator worker leaves agent execution to the shared
		// pool on the next tick, preserving the bookkeeping lane boundary.
		if t.slot != nil && t.slot == d.coordinator && d.graphStepRunsAgent(ctx, dispatch) {
			return
		}
		if t.capacity != nil {
			t.capacity.Release()
		}
		session, err := d.db.GetSession(ctx, dispatch.SessionID)
		if err != nil {
			t.recordWorkerError(err)
			t.log.ErrorContext(ctx, "read the session of the owed graph stage", slog.Any("error", err))
			return
		}
		log := t.log.With(slog.Int64("graph_dispatch_id", dispatch.ID), slog.String("graph_node", dispatch.Node))
		issue, err := d.readIssue(ctx, t.forgeRepo, session.IssueNumber)
		if err != nil {
			d.rateLimited(ctx, log, t.tickAt, err)
			log.ErrorContext(ctx, "graph step issue observation failed; leaving it to the tick", slog.Any("error", err))
			return
		}
		bugParent, lookupErr := d.isBugParentDispatch(ctx, dispatch)
		if lookupErr != nil {
			t.recordWorkerError(lookupErr)
			return
		}
		if issue.State != github.StateOpen && !bugParent {
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
		reservation, reason, err := d.graphStepSlot(ctx, dispatch)
		if err != nil {
			t.recordWorkerError(err)
			return
		}
		if reservation == nil {
			log.InfoContext(ctx, "graph step deferred for capacity", slog.String("reason", reason))
			return
		}
		reserved, ok, err := d.db.ReserveGraphDispatch(ctx, dispatch.ID, at)
		if err != nil || !ok {
			reservation.Release()
			if err != nil {
				t.recordWorkerError(err)
			}
			return
		}
		log.InfoContext(ctx, "running the owed graph stage in the same worker")
		step := &task{repo: t.repo, forgeRepo: t.forgeRepo, session: session, ref: t.ref, issue: issue, log: log, tickAt: t.tickAt, worker: t.worker, workerLog: t.workerLog, slot: reservation.pool, capacity: reservation}
		func() { defer reservation.Release(); d.runGraphStep(ctx, step, reserved) }()
		if step.graphDeferred {
			return
		}
	}
}

// runGraphStep executes one owed stage. The dispatch's private trigger is what
// gives this run its own identity on the runs table, the same way automatic
// recovery and manual schedules do; without one it could not exist beside the
// comment-triggered run that parked it.
func (d *Daemon) runGraphStep(ctx context.Context, t *task, dispatch store.GraphDispatch) {
	if d.bug != nil {
		if err := d.bug.CheckExistingAdmission(ctx, dispatch.SessionID); err != nil {
			t.recordWorkerError(err)
			return
		}
	}

	validating, modeErr := d.validateDispatch(ctx, dispatch)
	if modeErr != nil {
		t.recordWorkerError(modeErr)
		return
	}
	t.validating = validating
	// A Validate dispatch whose node is no longer where the instance rests
	// (it moved on, finished, or was replaced) is stale. Drop it quietly:
	// running it would offer its synthetic trigger to the open owner wait.
	if validating {
		if staleErr := d.db.ValidateGraphDispatchTarget(ctx, dispatch.SessionID, dispatch.TriggerID()); errors.Is(staleErr, store.ErrGraphInvariant) {
			t.log.InfoContext(ctx, "dropped a stale graph dispatch", slog.Int64("graph_dispatch_id", dispatch.ID), slog.String("graph_node", dispatch.Node), slog.String("cause", staleErr.Error()))
			if finishErr := d.db.FinishGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC()); finishErr != nil {
				t.recordWorkerError(finishErr)
			}
			return
		} else if staleErr != nil {
			t.recordWorkerError(staleErr)
			return
		}
	}
	trigger, command := graphStepRequest(dispatch, d.ticketIdentities(ctx).OwnerHandle)
	previousTrigger := t.commentUnderWay
	t.commentUnderWay = trigger.CommentID
	err := d.run(ctx, t, trigger, command, t.log)
	t.commentUnderWay = previousTrigger
	if t.graphDeferred && err == nil {
		t.recordWorkerStatus(workerDeferred)
		return
	}
	if err != nil {
		t.recordWorkerError(err)
	}
	admission, admissionErr := d.db.GetTaskWorkflowAdmission(ctx, dispatch.SessionID)
	if admissionErr != nil && !errors.Is(admissionErr, store.ErrNotFound) {
		t.recordWorkerError(admissionErr)
		return
	}
	if admissionErr == nil && admission.Workflow == "bug" && d.bug != nil {
		// A step refused before the Bug runtime took it (locked task, or an
		// admission error) still holds its reservation; requeue it so the
		// owed step is retried instead of staying reserved for good. The
		// store only requeues an unallocated reservation, so a step the
		// runtime already closed is untouched.
		if t.bugDispatchLocked || err != nil {
			cause := "Bug parent waits for task ownership"
			if !t.bugDispatchLocked {
				cause = err.Error()
			}
			_, retryErr := d.requeueGraphStep(ctx, dispatch, t.tickAt.UTC().Add(store.GraphDispatchRetryDelay(dispatch.Attempts)), cause)
			if retryErr != nil {
				t.recordWorkerError(retryErr)
			}
		}
		// The Bug runtime closes the exact dispatch as part of its persisted
		// transition/suspension. Factory-only nodes deliberately have no agent
		// run, so the mode driver must not interpret that absence as nonexecution.
		return
	}
	run, exists, lookupErr := d.db.GetRunByTrigger(ctx, dispatch.SessionID, dispatch.TriggerID())
	if lookupErr != nil {
		t.recordWorkerError(lookupErr)
		t.log.ErrorContext(ctx, "read graph step outcome", slog.Any("error", lookupErr))
		return
	}
	// The Implement coordinator runs no agent, so a step of it has no run by design.
	// Validate factory stages run no agent either. Either ran if it did not
	// fail; the instance records where it rests.
	if !exists && err == nil {
		runless := validating
		if !runless {
			var runlessErr error
			runless, runlessErr = d.implementCoordinatorDispatch(ctx, dispatch)
			if runlessErr != nil {
				t.recordWorkerError(runlessErr)
				return
			}
		}
		if runless {
			if finishErr := d.db.FinishGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC()); finishErr != nil {
				t.recordWorkerError(finishErr)
			}
			return
		}
	}
	// A node limit can fail the instance before allocating a run. Such work
	// owes a stop notice, not a retry against a terminal instance.
	if !exists || run.Status == store.RunPrelaunch {
		cause := ""
		if err != nil {
			cause = err.Error()
		}
		instance, instanceErr := d.db.GetGraphInstance(ctx, dispatch.InstanceID)
		if instanceErr != nil {
			t.recordWorkerError(instanceErr)
			return
		}
		if instance.Status == store.GraphFailed {
			if cause == "" {
				cause = "graph instance failed before the step could start"
			}
			if cancelErr := d.db.CancelGraphDispatch(ctx, dispatch.ID, cause, t.tickAt.UTC()); cancelErr != nil {
				t.recordWorkerError(cancelErr)
				return
			}
			stopped, stopErr := d.db.LatestGraphDispatch(ctx, dispatch.InstanceID)
			if stopErr == nil && stopped.ID == dispatch.ID && stopped.Status == "cancelled" && stopped.AllocatedRunID == 0 {
				stopErr = d.reportGraphDispatchStop(ctx, stopped)
			}
			if stopErr != nil {
				t.recordWorkerError(stopErr)
			}
			return
		}
		t.recordWorkerStatus(workerDeferred)
		retryAt := t.tickAt.UTC().Add(store.GraphDispatchRetryDelay(dispatch.Attempts))
		requeued, requeueErr := d.requeueGraphStep(ctx, dispatch, retryAt, cause)
		if requeueErr != nil {
			t.recordWorkerError(requeueErr)
			t.log.ErrorContext(ctx, "requeue graph step", slog.Any("error", requeueErr))
			return
		}
		if !requeued {
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

// A closed Bug issue still owes integration/closure observations and reports.
// The domain runtime owns those obligations. A dispatch replacing a cancelled
// intent still requires an open issue; recovery supplies no repair approval.
func (d *Daemon) isBugParentDispatch(ctx context.Context, dispatch store.GraphDispatch) (bool, error) {
	a, err := d.db.GetTaskWorkflowAdmission(ctx, dispatch.SessionID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil || a.Workflow != "bug" {
		return false, err
	}
	recovered, err := d.db.GraphDispatchFollowsCancellation(ctx, dispatch)
	return !recovered, err
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

// The store distinguishes an actual stop from an ineligible requeue. Closure
// cancellation never calls this path and therefore never announces recovery.
func (d *Daemon) requeueGraphStep(ctx context.Context, dispatch store.GraphDispatch, retryAt time.Time, cause string) (bool, error) {
	requeued, stopped, err := d.db.RequeueGraphDispatchWithStop(ctx, dispatch.ID, retryAt, cause)
	if err != nil || stopped == nil {
		return requeued, err
	}
	return false, d.reportGraphDispatchStop(ctx, *stopped)
}

func (d *Daemon) reportGraphDispatchStop(ctx context.Context, stopped store.GraphDispatch) error {
	reporter, ok := d.reporter.(interface {
		ReportGraphDispatchStop(context.Context, store.GraphDispatch) (turn.Posted, error)
	})
	if !ok {
		return errors.New("graph dispatch stop requires durable reporting")
	}
	_, err := reporter.ReportGraphDispatchStop(ctx, stopped)
	return err
}

// validateDispatch reports whether a dispatch belongs to a Validate instance, so
// its step runs in Validate whatever mode the issue's labels name.
func (d *Daemon) validateDispatch(ctx context.Context, dispatch store.GraphDispatch) (bool, error) {
	instance, err := d.db.GetGraphInstance(ctx, dispatch.InstanceID)
	if err != nil {
		return false, err
	}
	definition, err := d.db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return false, err
	}
	return definition.Mode == string(agent.ModeValidate), nil
}

// implementCoordinatorDispatch reports whether a dispatch belongs to an Implement
// coordinator: an instance of the Implement graph on a session that is not one
// story's worker. Such a step runs no agent and so owns no run.
func (d *Daemon) implementCoordinatorDispatch(ctx context.Context, dispatch store.GraphDispatch) (bool, error) {
	instance, err := d.db.GetGraphInstance(ctx, dispatch.InstanceID)
	if err != nil {
		return false, err
	}
	definition, err := d.db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return false, err
	}
	if definition.Mode != string(agent.ModeImplement) {
		return false, nil
	}
	if _, err := d.db.ImplementStoryByWorkerSession(ctx, dispatch.SessionID); err == nil {
		return false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	return true, nil
}

// Reconciliation owns no execution: only an idle task can surrender its stale
// dispatch. Both startup and ticks use the same durable and in-memory fences.
func (d *Daemon) reconcileGraphDispatches(ctx context.Context, at time.Time) error {
	interrupted, err := d.db.ListInterruptedGraphDispatches(ctx)
	if err != nil {
		return err
	}
	var faults error
	for _, dispatch := range interrupted {
		if !d.enterDoctorGuarded(ctx, dispatch.SessionID) {
			continue
		}
		err := d.reconcileGraphDispatch(ctx, dispatch, at)
		d.inflight.leave(dispatch.SessionID)
		faults = errors.Join(faults, err)
	}
	stranded, err := d.db.ListStrandedGraphDispatches(ctx)
	if err != nil {
		return errors.Join(faults, err)
	}
	for _, dispatch := range stranded {
		if !d.enterDoctorGuarded(ctx, dispatch.SessionID) {
			continue
		}
		err := d.reconcileGraphDispatch(ctx, dispatch, at)
		d.inflight.leave(dispatch.SessionID)
		faults = errors.Join(faults, err)
	}
	return faults
}

func (d *Daemon) reconcileGraphDispatch(ctx context.Context, dispatch store.GraphDispatch, at time.Time) (err error) {
	locked, err := d.db.AcquireSessionLock(ctx, dispatch.SessionID, d.instanceID)
	if err != nil || !locked {
		return err
	}
	defer func() {
		err = errors.Join(err, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), dispatch.SessionID, d.instanceID))
	}()
	if dispatch.ID == 0 {
		if err := turn.ValidateGraphDispatchRecovery(ctx, d.db, dispatch); err != nil {
			d.log.WarnContext(ctx, "stranded graph capture cannot resume; retained work preserved", slog.Int64("instance_id", dispatch.InstanceID), slog.Any("error", err))
			return nil
		}
		return d.db.ReconcileStrandedGraphDispatch(ctx, dispatch, at)
	}
	capture := int64(0)
	if validationErr := turn.ValidateGraphDispatchRecovery(ctx, d.db, dispatch); validationErr == nil {
		capture = dispatch.LaunchID
	} else {
		d.log.WarnContext(ctx, "interrupted graph dispatch capture cannot resume; retained work preserved", slog.Int64("dispatch_id", dispatch.ID), slog.Any("error", validationErr))
	}
	return d.db.ReconcileGraphDispatch(ctx, dispatch.ID, capture, at)
}
