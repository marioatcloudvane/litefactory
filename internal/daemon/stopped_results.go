package daemon

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// stoppedResults retains positive process-completion evidence even when every
// database write fails. Task ownership survives; worker capacity does not.
// Restart deliberately falls back to exclusive orphan reconciliation.
type stoppedResults struct {
	mu    sync.Mutex
	items map[int64]*stoppedResult
}

type stoppedResult struct {
	task    task
	repair  func(context.Context) (turn.Result, error)
	trigger turn.Trigger
	mode    agent.Mode
	active  bool
}

func (d *Daemon) retainStoppedResult(t *task, repair func(context.Context) (turn.Result, error), trigger turn.Trigger, mode agent.Mode) {
	d.stoppedDebt.mu.Lock()
	defer d.stoppedDebt.mu.Unlock()
	if d.stoppedDebt.items == nil {
		d.stoppedDebt.items = make(map[int64]*stoppedResult)
	}
	d.stoppedDebt.items[t.session.ID] = &stoppedResult{task: *t, repair: repair, trigger: trigger, mode: mode, active: true}
}

// releaseWorker transfers ownership to repair debt only after its worker body
// has finished. A concurrent tick cannot run the repair against live cleanup.
// owes reports whether a session carries a stopped result not yet persisted.
// Nothing else may run on that session until its repair succeeds.
func (s *stoppedResults) owes(sessionID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[sessionID] != nil
}

func (s *stoppedResults) releaseWorker(sessionID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	debt := s.items[sessionID]
	if debt == nil {
		return false
	}
	debt.active = false
	return true
}

func (d *Daemon) retryStoppedResults(ctx context.Context, tickAt time.Time) (dispatched int) {
	if _, held := d.backoff.hold(tickAt); held || ctx.Err() != nil {
		return
	}
	d.stoppedDebt.mu.Lock()
	defer d.stoppedDebt.mu.Unlock()
	ids := make([]int64, 0, len(d.stoppedDebt.items))
	for id := range d.stoppedDebt.items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		debt := d.stoppedDebt.items[id]
		if debt.active {
			continue
		}
		source := stoppedRecovery(debt)
		allowed, err := d.guardedRecoveryAllowed(ctx, source, tickAt)
		if err != nil {
			d.recoveryFailure(ctx, source, tickAt, err)
			continue
		}
		if !allowed {
			continue
		}
		if !d.pool.try() {
			return
		}
		debt.active = true
		t := debt.task
		t.tickAt = tickAt
		workerCtx := context.WithValue(ctx, recoveryTickKey{}, tickAt)
		// Existing pool and Quiesce bound this worker exactly like execution.
		t.workerLog.DebugContext(ctx, "stopped-result repair worker dispatched")
		go d.runDoctorGuardedWorker(workerCtx, &t, func(workerCtx context.Context) { d.repairStoppedResult(workerCtx, &t, debt) })
		dispatched++
	}
	return dispatched
}

func (d *Daemon) repairStoppedResult(ctx context.Context, t *task, debt *stoppedResult) {
	result, err := debt.repair(ctx)
	if err != nil {
		t.recordWorkerError(err)
		d.recoveryFailure(ctx, stoppedRecovery(debt), t.tickAt, err)
		return
	}
	d.recoverySucceeded(ctx, stoppedRecovery(debt).Key)
	d.stoppedDebt.mu.Lock()
	delete(d.stoppedDebt.items, t.session.ID)
	d.stoppedDebt.mu.Unlock()
	if err := d.completeTurn(ctx, t, debt.trigger, debt.mode, result, t.log); err != nil {
		t.recordWorkerError(err)
	}
	if t.scheduled != nil {
		status := store.ScheduleFailed
		if result.Run.Succeeded() {
			status = store.ScheduleSucceeded
		}
		current, err := d.db.GetSchedule(ctx, t.scheduled.ID)
		if err != nil {
			t.recordWorkerError(err)
			t.log.ErrorContext(ctx, "could not read repaired schedule outcome", slog.Any("error", err))
			return
		}
		if current.Status == store.ScheduleRunning {
			t.tickAt = result.Run.FinishedAt
			d.finishScheduled(ctx, t, *t.scheduled, status, result.Run.ID)
		}
	}
}

func stoppedRecovery(debt *stoppedResult) store.ReportRecovery {
	return store.ReportRecovery{Key: "stopped:" + recoveryKey(debt.task.session.ID, debt.trigger.CommentID), SessionID: debt.task.session.ID, TriggerID: debt.trigger.CommentID, Task: debt.task.ref.String(), State: "stopped"}
}
