// Package cimonitor provides bounded, read-only factory CI observation.
package cimonitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// DeferredError reports durable admission limits rather than sleeping or retrying.
type DeferredError struct{ Until time.Time }

func (e *DeferredError) Error() string {
	return "CI request deferred until " + e.Until.UTC().Format(time.RFC3339)
}

// TransportUncertainError reports a request whose response timing was not
// durably recorded. Lease expiry cannot prove admission is safe.
type TransportUncertainError struct{}

func (*TransportUncertainError) Error() string {
	return "CI transport timing uncertain after interrupted request"
}

type pendingTiming struct {
	lease      string
	bound      time.Time
	provenance string
	limited    bool
}

// Gate serializes CI requests, including doctor probes, through durable waits.
// It adds no factory-wide quota or retry policy.
type Gate struct {
	Store   *store.DB
	Key     string
	Now     func() time.Time
	mu      sync.Mutex
	pending *pendingTiming
}

func (g *Gate) clock() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Do admits one read and retains every applicable timing bound before release.
func (g *Gate) Do(ctx context.Context, request func(context.Context) (github.ActionsTiming, error)) error {
	if g == nil || g.Store == nil || g.Key == "" || request == nil {
		return fmt.Errorf("CI transport unavailable")
	}
	// A diagnostic must not wait on another caller beyond its own deadline.
	if !g.mu.TryLock() {
		return &DeferredError{Until: g.clock().Add(3 * time.Minute)}
	}
	defer g.mu.Unlock()
	// A prior request may have returned timing that could not be journaled.
	// Retry that local write first; never repeat its HTTP request.
	if g.pending != nil {
		if err := g.finish(ctx, g.pending); err != nil {
			return err
		}
		g.pending = nil
	}
	now := g.clock()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	lease := hex.EncodeToString(nonce[:])
	state, admitted, err := g.Store.ClaimCITransport(ctx, g.Key, lease, now, now.Add(3*time.Minute))
	if err != nil {
		return fmt.Errorf("CI transport state unavailable: %w", err)
	}
	if !admitted {
		if state.Lease != "" && !state.LeaseUntil.After(now) {
			return &TransportUncertainError{}
		}
		until := state.NotBefore
		if state.Lease != "" && state.LeaseUntil.After(until) {
			until = state.LeaseUntil
		}
		return &DeferredError{Until: until}
	}
	// The bounded request deadline identifies an abandoned lease without
	// treating its expiration as permission to discard unknown response timing.
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	timing, requestErr := request(reqCtx)
	cancel()
	now = g.clock()
	bound := timing.RetryAfter
	provenance := "retry_after"
	if timing.RateLimitReset.After(bound) {
		bound = timing.RateLimitReset
		provenance = "rate_limit_reset"
	}
	if poll := now.Add(timing.PollInterval); timing.PollInterval > 0 && poll.After(bound) {
		bound = poll
		provenance = "poll_interval"
	}
	limited := timing.RateLimited || errors.Is(requestErr, github.ErrRateLimited)
	if limited {
		// GitHub can omit timing headers on secondary limits. This fallback is
		// local to CI and never shortens an explicit server bound.
		wait := time.Minute
		for i := 0; i < state.Failures && wait < time.Hour; i++ {
			wait *= 2
		}
		if wait > time.Hour {
			wait = time.Hour
		}
		if fallback := now.Add(wait); fallback.After(bound) {
			bound = fallback
			provenance = "ci_rate_limit_fallback"
		}
	}
	// Preserve the received timing in memory until the durable handoff succeeds.
	// The retained lease prevents other processes from making a new request.
	g.pending = &pendingTiming{lease: lease, bound: bound, provenance: provenance, limited: limited}
	if err := g.finish(ctx, g.pending); err != nil {
		return errors.Join(requestErr, err)
	}
	g.pending = nil
	return requestErr
}

func (g *Gate) finish(ctx context.Context, pending *pendingTiming) error {
	// Persist even when the bounded request was canceled. This timeout owns only
	// transport writes; incident/recovery state is inaccessible to this gate.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	_, err := g.Store.FinishCITransport(saveCtx, g.Key, pending.lease, pending.bound, pending.provenance, pending.limited)
	if err != nil {
		return fmt.Errorf("CI transport wait could not be retained: %w", err)
	}
	return nil
}
