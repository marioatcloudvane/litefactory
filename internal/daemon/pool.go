package daemon

import (
	"context"
	"errors"
	"sync"
)

// pool bounds live execution costs. Its mutex makes a weighted acquisition
// atomic; its independent join tracks workers through cleanup and diagnostics.
// There is no waiting queue: refused work remains with the scheduler.
type pool struct {
	mu    sync.Mutex
	slots chan struct{}
	wg    sync.WaitGroup
}

// newPool builds a pool with size execution slots.
//
// size is [Options.MaxConcurrentRuns] after defaulting, which
// [Options.validate] has already confined to the supported range. It is
// floored here rather than trusted anyway, because the one value that would do
// real damage is a non-positive one: that is a channel of capacity zero, a
// pool that can never dispatch anything, and a daemon that looks alive and
// does nothing forever.
func newPool(size int) *pool {
	if size < MinConcurrentRuns {
		size = MinConcurrentRuns
	}
	return &pool{slots: make(chan struct{}, size)}
}

// try is the legacy unit-cost worker admission. It registers the join only
// after reserving capacity, and is paired with done on abandoned launches.
func (p *pool) try() bool {
	if !p.acquire(1) {
		return false
	}
	p.wg.Add(1)
	return true
}

func (p *pool) acquire(cost int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cost < 0 || cost > cap(p.slots)-len(p.slots) {
		return false
	}
	for range cost {
		p.slots <- struct{}{}
	}
	return true
}

// replace releases a completed step and takes the next cost under one lock.
// Refusal leaves no partial reservation behind.
func (p *pool) replace(previous, cost int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for range previous {
		<-p.slots
	}
	if cost > cap(p.slots)-len(p.slots) {
		return false
	}
	for range cost {
		p.slots <- struct{}{}
	}
	return true
}

func (p *pool) release(cost int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for range cost {
		<-p.slots
	}
}

func (p *pool) capacity() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.slots), cap(p.slots)
}

func (p *pool) done() {
	p.release(1)
	p.wg.Done()
}

// stepReservation can change the executing cost independently of the worker
// join. A turn releases it after its activity and cleanup; worker unwinding also
// releases it, including abandoned launches and panics. Release is idempotent.
type stepReservation struct {
	mu       sync.Mutex
	pool     *pool
	fallback *pool
	cost     int
}

func (r *stepReservation) Replace(ctx context.Context, cost int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if cost < 0 {
		return false, errors.New("negative execution slot cost")
	}
	if r.cost == cost {
		return true, nil
	}
	admitted := r.pool.replace(r.cost, cost)
	r.cost = 0
	if !admitted && r.fallback != nil && r.fallback != r.pool && r.fallback.acquire(cost) {
		r.pool = r.fallback
		admitted = true
	}
	if !admitted {
		return false, nil
	}
	r.cost = cost
	return true, nil
}
func (r *stepReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pool.release(r.cost)
	r.cost = 0
}
func (r *stepReservation) Capacity() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cost == 0 && r.fallback != nil {
		return r.fallback.capacity()
	}
	return r.pool.capacity()
}

// wait blocks until every worker has finished. It takes no context, and that
// is the point.
//
// It is the shutdown join, called from loop's defer, and by then the loop's
// context is *already cancelled* — being cancelled is why loop is returning.
// Written as select { case <-drained: case <-ctx.Done(): } this would therefore
// return immediately at the only moment it is ever called, abandoning every
// worker: a join that is a no-op and still reads like a join. Do not "fix" it
// into a select.
//
// Waiting without a bound is not the hazard it looks like, because this is not
// where the shutdown budget lives. [Daemon.Run] holds that budget: it waits on
// done for [Options.ShutdownGrace] and returns regardless, leaving anything
// still running to be abandoned and reconciled by the next start (D-B,
// AC-2.21). What this function does is make the difference between "finished"
// and "abandoned" a real one for every worker that can still make it.
//
// A caller that has a live context and wants to give up on waiting wants
// [Daemon.Quiesce] instead.
func (p *pool) wait() { p.wg.Wait() }

// quiesce waits for the pool to drain or for ctx to be done, whichever happens
// first. It is [Daemon.Quiesce]; see there for when it may be called.
func (p *pool) quiesce(ctx context.Context) error {
	drained := make(chan struct{})
	// The only goroutine in this file that is not a turn worker, and the only
	// one not in the RD-10 inventory above — because it is not the daemon's. It
	// exists for the duration of one quiesce call, holds nothing and decides
	// nothing, and it ends when the pool drains, which the shutdown join makes
	// certain happens. Production shutdown goes through wait and starts
	// nothing.
	go func() {
		defer close(drained)
		p.wg.Wait()
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Quiesce blocks until every turn this daemon has in flight has finished, or
// until ctx is done — whichever comes first. A context that is cancelled or
// expires first comes back as ctx.Err(); a daemon with nothing in flight
// returns nil immediately.
//
// This is the only exported API concurrency adds to the daemon, and it is
// meant as one rather than tolerated as a test seam: "wait until this daemon is
// idle" is the one question about concurrency a caller outside the package can
// ask without being told how the pool works. It is what lets a test assert on
// what a tick did with no sleep and no polling, and it is the natural place to
// hang a later "what is in flight" view (LF-11) — extend it rather than adding
// a second exported way to ask the same thing.
//
// Call it between ticks rather than during one. It is safe from any goroutine,
// but a dispatch starting at the same instant is a WaitGroup Add racing a Wait,
// which is a misuse the runtime is entitled to panic on.
//
// Shutdown deliberately does not come through here: see [pool.wait] for why
// that path must not take a context at all.
func (d *Daemon) Quiesce(ctx context.Context) error {
	err := d.pool.quiesce(ctx)
	if d.doctor.pool != nil {
		err = errors.Join(err, d.doctor.pool.quiesce(ctx))
	}
	if d.coordinator != nil {
		err = errors.Join(err, d.coordinator.quiesce(ctx))
	}
	return err
}

// runSlots lends the daemon's run-slot pool to work a running turn starts by
// itself, such as the Validate reviewers, so it counts against
// max_concurrent_runs like any dispatched turn. It is built before the daemon
// that owns the pool, and bound to that pool once it exists.
//
// A reviewer asks from inside a turn worker, which already holds a slot of the
// same pool, so its Add never takes the join's counter off zero and cannot race
// [pool.wait].
type runSlots struct {
	mu   sync.Mutex
	pool *pool
}

func (s *runSlots) bind(p *pool) {
	s.mu.Lock()
	s.pool = p
	s.mu.Unlock()
}

// TryAcquire takes a slot without waiting; ok is false when none is free.
func (s *runSlots) TryAcquire(ctx context.Context) (func(), bool, error) {
	return s.TryAcquireWeighted(ctx, 1)
}

func (s *runSlots) Capacity() (int, int) {
	s.mu.Lock()
	p := s.pool
	s.mu.Unlock()
	if p == nil {
		return 0, 0
	}
	return p.capacity()
}

func (s *runSlots) TryAcquireWeighted(ctx context.Context, cost int) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	p := s.pool
	s.mu.Unlock()
	if p == nil {
		return nil, false, errors.New("the run-slot pool is not running")
	}
	if cost <= 0 {
		return nil, false, errors.New("execution slot cost must be positive")
	}
	if !p.acquire(cost) {
		return nil, false, nil
	}
	p.wg.Add(1)
	var once sync.Once
	return func() { once.Do(func() { p.release(cost); p.wg.Done() }) }, true, nil
}
