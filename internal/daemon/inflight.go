package daemon

import (
	"context"
	"slices"
	"sync"
)

// inflight is the set of sessions this daemon process is running a turn for
// right now, plus the largest that set has ever been.
//
// It is the unit of exclusion for RD-3: a session is entered before its turn is
// dispatched and left after its report has posted, so everything between —
// dispatch, drain, the last report, the watermark advance — happens for one
// session at a time while different sessions proceed in parallel.
//
// # Two mechanisms, two purposes
//
// litefactory now has two ways of saying "not this session, not now", and they
// are not redundant.
//
// The durable one is sessions.lock_owner in the database (unchanged by this
// feature). It is held across [turn.Execute] only, it survives a crash, and it
// is cross-process: it is what stops a second daemon *process* from touching
// the same worktree and the same runs row.
//
// The in-memory one is this set. It is scoped wider than the store lock —
// dispatch through to the watermark advance — and narrower in reach: it means
// nothing outside this process. What it protects is thread ordering and
// duplicate dispatch *within* this process, which is exactly what the store
// lock does not cover, because the store lock is released before the report is
// rendered and posted.
//
// # A load-bearing side effect
//
// Because the set is checked-and-inserted on the Tick goroutine, and Tick is
// never called concurrently with itself, two sibling workers can never both
// reach AcquireSessionLock for one session. Say that plainly, because the
// obvious reading of the code is the wrong one: the database lock is *not*
// what serialises the workers of a single daemon. It has become defence in
// depth against a dispatcher bug, and the primary intra-process mechanism is
// this set.
//
// An inflight is safe for concurrent use, and its zero value is an empty set.
type inflight struct {
	mu        sync.Mutex
	sessions  map[int64]struct{}
	cancels   map[int64]context.CancelFunc
	settled   map[int64]chan struct{}
	highWater int
}

// enter takes the session, reporting whether this caller is the one that got
// it. A false return means a turn for that session is already in flight and
// the caller must not dispatch: it is a check-and-insert under one lock, not a
// query the caller may act on afterwards.
//
// Every true return is owed exactly one [inflight.leave].
func (f *inflight) enter(sessionID int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, busy := f.sessions[sessionID]; busy {
		return false
	}
	if f.sessions == nil {
		f.sessions = make(map[int64]struct{})
	}
	f.sessions[sessionID] = struct{}{}
	if f.settled == nil {
		f.settled = make(map[int64]chan struct{})
	}
	f.settled[sessionID] = make(chan struct{})
	if len(f.sessions) > f.highWater {
		f.highWater = len(f.sessions)
	}
	return true
}

// leave releases a session entered by [inflight.enter]. Leaving a session that
// is not in the set is a no-op rather than a panic: leave is called from a
// worker's defer, which is the one place that must not turn a bug into a dead
// daemon.
func (f *inflight) leave(sessionID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, sessionID)
	delete(f.cancels, sessionID)
	if done := f.settled[sessionID]; done != nil {
		close(done)
		delete(f.settled, sessionID)
	}
}

// size reports how many sessions are in flight at this instant.
func (f *inflight) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

// list reports the sessions in flight at this instant, ordered by id.
func (f *inflight) list() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]int64, 0, len(f.sessions))
	for id := range f.sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// peak reports the most sessions that have been in flight at once since the
// daemon started.
//
// It is the daemon's own record of how much of its concurrency it actually
// used — a number for the operator, not a test hook. What a test asserts on is
// the concurrency it *observes* from inside the executor (AC-PAR-1.9); a
// counter maintained here could only ever agree with the code that maintains
// it.
func (f *inflight) peak() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.highWater
}

func (f *inflight) bindCancel(session int64, cancel context.CancelFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancels == nil {
		f.cancels = make(map[int64]context.CancelFunc)
	}
	if _, ok := f.sessions[session]; ok {
		f.cancels[session] = cancel
	}
}
func (f *inflight) drain(ctx context.Context, session int64, interrupt bool) error {
	f.mu.Lock()
	done := f.settled[session]
	cancel := f.cancels[session]
	f.mu.Unlock()
	if interrupt && cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cancel observes active work and optionally interrupts it without blocking the
// control lane on finalization. Finished runs only need the settlement check.
func (f *inflight) cancel(session int64, interrupt bool) bool {
	f.mu.Lock()
	cancel := f.cancels[session]
	_, busy := f.sessions[session]
	f.mu.Unlock()
	if interrupt && cancel != nil {
		cancel()
	}
	return busy
}
