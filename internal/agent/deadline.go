package agent

import (
	"context"
	"sync"
	"time"
)

type workerDeadlineKey struct{}

type workerDeadlineState struct {
	evidenceOnce     sync.Once
	evidenceDeadline time.Time
}

// WithWorkerDeadline marks the factory's resolved absolute worker deadline.
// Provider adapters must use this deadline instead of their compatibility timer.
func WithWorkerDeadline(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(ctx, deadline)
	return context.WithValue(ctx, workerDeadlineKey{}, &workerDeadlineState{}), cancel
}

// InvocationContext retains the factory deadline when one was resolved. Direct
// adapter callers retain their configured compatibility allowance.
func InvocationContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Value(workerDeadlineKey{}).(*workerDeadlineState); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, fallback)
}

// EvidenceContext keeps live collection within the worker deadline. Once the
// worker stops, the first collector starts one independent allowance shared by
// all later collectors. Remote delivery uses its own context.
func EvidenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(30 * time.Second)
	if worker, ok := ctx.Value(workerDeadlineKey{}).(*workerDeadlineState); ok {
		workerDeadline, hasDeadline := ctx.Deadline()
		if ctx.Err() == nil && (!hasDeadline || time.Now().Before(workerDeadline)) {
			return context.WithTimeout(ctx, 30*time.Second)
		}
		worker.evidenceOnce.Do(func() { worker.evidenceDeadline = deadline })
		deadline = worker.evidenceDeadline
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

// TimeoutSettlementContext bounds durable local bookkeeping after recorded
// expiry. It cannot run provider work, collect fresh evidence or deliver a
// remote notice; those retain their separate execution and recovery contexts.
func TimeoutSettlementContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
