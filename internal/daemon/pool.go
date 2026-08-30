package daemon

import (
	"context"
	"sync"
)

// pool is the bound on how many turns run at the same instant, and the join
// that guarantees none of them outlives the loop that dispatched them.
//
// # Why a semaphore and not a fixed set of workers
//
// A fixed worker set needs a work channel, and a work channel is a queue. A
// queue commits a task to a worker when it is enqueued and dispatches it some
// time later, under fairness state and backoff state that have both gone stale
// in between (RD-1, AC-PAR-4.4), and it outlives the tick that filled it. This
// semaphore has no queue: a dispatch either starts now or its session is left
// needing a poll and reconsidered from scratch, against fresh state, on the
// next tick.
//
// # The goroutine budget (RD-10)
//
// How many: four — two in the harness, one per pipe of the claude child; this
// daemon's loop; and main's second-signal watcher — plus at most
// max_concurrent_runs turn workers. That setting is validated against
// [MinConcurrentRuns]..[MaxConcurrentRuns] and refused, never clamped, outside
// it (AC-PAR-6.1), so the inventory has a hard ceiling of twenty.
//
// Termination condition, as one named thing: [Daemon.Quiesce]. Every worker
// ends on one of three bounded conditions — its comment list is exhausted, a
// comment was left unconsumed, or the context is done — and all three are
// joined there and, at shutdown, by the unexported [pool.wait] underneath it.
//
// What happens when the bound is exceeded: it cannot be. The semaphore is a
// buffered channel made once in [New], with exactly one send site (the dispatch
// loop, on the Tick goroutine) and exactly one receive site (the worker's
// defer). The send is a select with a default, so a full pool refuses a
// dispatch instead of parking discovery behind a turn.
//
// What the goroutines buy: the wall-clock of a turn minus its git critical
// sections, overlapped across sessions. A 30-minute run stops delaying
// discovery by 30 minutes.
type pool struct {
	// slots is the semaphore: a value in the channel is a slot in use, so a
	// send acquires and a receive releases.
	slots chan struct{}
	// wg counts the same workers the semaphore does. Both are needed because a
	// buffered channel's occupancy cannot be waited on and a WaitGroup cannot
	// be tested without blocking.
	wg sync.WaitGroup
}

// newPool builds a pool that will run at most size turns at once.
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

// try takes a slot without blocking, reporting whether it got one.
//
// False means the pool is full. The caller must not dispatch; the session it
// was about to dispatch is deferred to the next tick rather than queued, for
// the reason in the type comment.
//
// A true return has already registered the worker with the join, so it must be
// followed immediately by the `go` that runs it, whose *first* deferred call is
// [pool.done] — first, so that it runs last, after every other defer the worker
// registers. A true return that never reaches a `go` leaks a slot for the life
// of the process.
//
// try is called only from the dispatch loop, on the Tick goroutine, and so is
// never concurrent with [pool.wait] or with itself. That is what satisfies
// sync.WaitGroup's rule that an Add taking the counter off zero must not race a
// Wait: loop waits only once the last Tick has returned.
func (p *pool) try() bool {
	select {
	case p.slots <- struct{}{}:
	default:
		return false
	}
	p.wg.Add(1)
	return true
}

// done releases a worker's slot and unregisters it from the join. It is the
// receive site of the semaphore and belongs in a defer, never on a return path.
//
// The slot is released before the count drops so that a returning [pool.wait]
// means both "every worker finished" and "every slot is free"; the other order
// would let the next dispatch be refused a slot the pool no longer holds.
func (p *pool) done() {
	<-p.slots
	p.wg.Done()
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
	return d.pool.quiesce(ctx)
}
