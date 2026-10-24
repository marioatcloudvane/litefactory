package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// workerStatus describes the worker, independently of any agent outcome.
type workerStatus uint8

const (
	workerCompleted workerStatus = iota
	workerDeferred
	workerFailed
	workerCancelled
)

func (s workerStatus) String() string {
	switch s {
	case workerDeferred:
		return "deferred"
	case workerFailed:
		return "failed"
	case workerCancelled:
		return "cancelled"
	default:
		return "completed"
	}
}

// workerState is shared by sequential task copies within one worker (notably
// refinement and schedule settlement), never by concurrent workers.
type workerState struct {
	status            workerStatus
	executionRequests int
}

func (t *task) recordWorkerStatus(status workerStatus) {
	if t.worker != nil && status > t.worker.status {
		t.worker.status = status
	}
}

// recordWorkerError preserves cancellation even when a dependency returns a
// cancellation result before the outer context observes it.
func (t *task) recordWorkerError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.recordWorkerStatus(workerCancelled)
		return
	}
	t.recordWorkerStatus(workerFailed)
}

// runWorker joins the body, cleanup and terminal diagnostic independently of
// its current step reservation. In-flight ownership survives until cleanup,
// unless stopped-result repair debt retains it. Panics unwind both capacity
// and the join through defers, without claiming normal completion.
func (d *Daemon) runWorker(ctx context.Context, t *task, body func()) {
	join := d.slotOf(t)
	if t.capacity == nil {
		t.capacity = &stepReservation{pool: join, cost: 1}
	}
	defer join.wg.Done()
	defer t.capacity.Release()
	if t.discoveryReservation != nil {
		defer t.discoveryReservation.finish()
	}
	started := time.Now() // Private monotonic diagnostic timer, never scheduling time.
	t.worker = &workerState{}
	func() {
		defer func() {
			if !d.stoppedDebt.releaseWorker(t.session.ID) {
				d.inflight.leave(t.session.ID)
			}
		}()
		body()
		// Still holding the session: owed graph stages follow without a tick.
		held, err := d.doctorHeldSession(ctx, t.session.ID)
		if err == nil && !held && !t.graphDeferred {
			d.continueGraphSteps(ctx, t)
		}
	}()
	if ctx.Err() != nil {
		t.recordWorkerStatus(workerCancelled)
	}
	t.workerLog.DebugContext(ctx, "worker finished",
		slog.String("status", t.worker.status.String()),
		slog.Duration("duration", time.Since(started)),
		slog.Int("execution_requests", t.worker.executionRequests))
}

// slotOf is the pool a task's worker holds: its reserved slot, or the run pool.
func (d *Daemon) slotOf(t *task) *pool {
	if t.slot != nil {
		return t.slot
	}
	return d.pool
}
