package agent

import (
	"context"
	"errors"
	"sync"
)

// ProviderObserver safely shares an optional terminal observation sink.
// It deliberately exposes a copy so observation cannot mutate run results.
type ProviderObserver struct {
	mu       sync.RWMutex
	callback func(context.Context, Request, *RunResult, error)
}

// Set replaces the sink; observer calls do not hold its lock during callbacks.
func (o *ProviderObserver) Set(callback func(context.Context, Request, *RunResult, error)) {
	o.mu.Lock()
	o.callback = callback
	o.mu.Unlock()
}

// Observe emits once at the adapter's terminal return, excluding unstarted work
// and shutdown/timeout cancellation. A finished unknown failure still emits.
func (o *ProviderObserver) Observe(ctx context.Context, request Request, result *RunResult, err error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if result != nil && (result.Outcome == OutcomeTimedOut || result.Outcome == OutcomeAbandoned) {
		return
	}
	if result == nil && ProviderOutcome(nil, err) == ProviderUnknown {
		return
	}
	o.mu.RLock()
	callback := o.callback
	o.mu.RUnlock()
	if callback != nil {
		callback(ctx, request, result.Clone(), err)
	}
}
