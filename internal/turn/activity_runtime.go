package turn

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// maxRuntimeActivities bounds both retained process-local owners and cleanup
// work. Production admits at most 16 concurrent workers; the extra space allows
// a replacement to revoke its predecessor before the predecessor exits.
const maxRuntimeActivities = 32

func (r *MCPRuntime) trackActivity(ctx context.Context, p *preparedMCP, now time.Time) error {
	r.activityMu.Lock()
	defer r.activityMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.activityClosed {
		return store.ErrResultAuthority
	}
	if err := p.guard.Check(ctx); err != nil {
		return err
	}
	for old := range r.activities {
		if old.guard.Binding().Task == p.guard.Binding().Task {
			if err := old.clearActivity(ctx, now); err != nil {
				return err
			}
			delete(r.activities, old)
		}
	}
	if len(r.activities) >= maxRuntimeActivities {
		return errors.New("activity runtime capacity reached")
	}
	if r.activities == nil {
		r.activities = make(map[*preparedMCP]struct{})
	}
	r.activities[p] = struct{}{}
	return nil
}

// clearActivity deliberately retains no terminal display. Explicit cleanup is
// permitted by the advisory contract and keeps retention bounded even without a
// future daemon tick. It never deletes durable receipts or publication intent.
func (p *preparedMCP) clearActivity(ctx context.Context, now time.Time) error {
	var lifecycleErr, revokeErr error
	if p.activity != nil {
		_, lifecycleErr = p.activity.Lifecycle(ActivityLifecycle{Now: now, Terminal: true, Clear: true})
	}
	if p.guard != nil {
		revokeErr = p.guard.Revoke(ctx, "closed")
	}
	return errors.Join(lifecycleErr, revokeErr)
}

func (p *preparedMCP) endActivity(ctx context.Context, now time.Time) error {
	p.runtime.activityMu.Lock()
	defer p.runtime.activityMu.Unlock()
	err := p.clearActivity(ctx, now)
	if err == nil {
		delete(p.runtime.activities, p)
	}
	return err
}

// CleanActivities is called by daemon scheduling with its current time. Each
// pass examines at most maxRuntimeActivities owners, uses existing lease guards,
// and starts no workers or timers. Revocation cancels in-flight calls; transport
// workers and their brokers remain joined by the normal attempt close path.
func (r *MCPRuntime) CleanActivities(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return errors.New("activity cleanup requires factory time")
	}
	r.activityMu.Lock()
	defer r.activityMu.Unlock()
	var errs []error
	for p := range r.activities {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		terminal := !now.Before(p.guard.Lease().ExpiresAt)
		if !terminal {
			err := p.guard.Check(ctx)
			if err != nil && !errors.Is(err, store.ErrResultAuthority) {
				errs = append(errs, err)
				continue
			}
			terminal = err != nil
		}
		if terminal {
			if err := p.clearActivity(ctx, now); err != nil {
				errs = append(errs, err)
			} else {
				delete(r.activities, p)
			}
		}
	}
	return errors.Join(errs...)
}

// Close retires process-local activity before closing runtime authority. Daemon
// shutdown owns this operation independently of any abandoned runner's return.
func (r *MCPRuntime) Close(ctx context.Context) error {
	r.activityMu.Lock()
	r.activityClosed = true
	var errs []error
	for p := range r.activities {
		if err := p.clearActivity(ctx, r.Now()); err != nil {
			errs = append(errs, err)
		} else {
			delete(r.activities, p)
		}
	}
	r.activityMu.Unlock()
	return errors.Join(append(errs, r.Authority.Close(ctx))...)
}
