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

// runWorker owns admission until the body and its cleanup return. The terminal
// diagnostic observes released in-flight ownership unless stopped-result repair
// debt retains it, but not a free pool slot:
// pool.done stays last, so Quiesce also joins the diagnostic. Panics unwind
// admission through the defers but are not represented as normal completion.
func (d *Daemon) runWorker(ctx context.Context, t *task, body func()) {
	defer d.pool.done()
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
		if err == nil && !held {
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
